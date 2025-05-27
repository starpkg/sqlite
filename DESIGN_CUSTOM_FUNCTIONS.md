# Custom SQL Function Registration Design

## Overview

This document describes the design and implementation of custom SQL function registration in the Starlark SQLite module. This feature allows users to register custom SQL functions written in Starlark that can be called from SQL queries.

**Important**: SQLite function registration in `modernc.org/sqlite` is package-wide and affects all database connections opened after registration. Therefore, function registration must be done at the module level, not per-database instance.

## API Design

### Function Signature

```python
sqlite.register_function(name, func, num_args=None, deterministic=False)
```

### Parameters

- `name` (string): The name of the SQL function to register
- `func` (callable): A Starlark function/lambda that implements the custom logic
- `num_args` (int, optional): Number of arguments the function accepts. If None/not specified, the function is variadic (-1)
- `deterministic` (bool, optional): Whether the function is deterministic (default: False)

### Return Value

Returns `None` on success, raises an error on failure.

## Usage Examples

### Basic Scalar Function

```python
load("sqlite", "connect", "register_function")

# Register function BEFORE opening any database connections
register_function("MY_TRIM", lambda s: s.strip() if s else "")

# Now open database and use the function
db = connect(":memory:")
db.execute("CREATE TABLE users (name TEXT)")
db.execute("INSERT INTO users VALUES ('  John Doe  ')")
result = db.query("SELECT MY_TRIM(name) as clean_name FROM users")
print(result)  # [{"clean_name": "John Doe"}]
```

### Multi-Argument Function

```python
load("sqlite", "connect", "register_function")

# Register a tax calculation function
register_function("ADD_TAX", lambda price, rate: price * (1.0 + rate), num_args=2)

# Use in any database connection opened after registration
db = connect(":memory:")
db.execute("CREATE TABLE products (price REAL)")
db.execute("INSERT INTO products VALUES (100.0)")
result = db.query("SELECT ADD_TAX(price, 0.08) as total FROM products")
print(result)  # [{"total": 108.0}]
```

### Deterministic Function

```python
load("sqlite", "connect", "register_function")

# Register a deterministic mathematical function
register_function("SQUARE", lambda x: x * x, num_args=1, deterministic=True)

# Use in SQL with optimization benefits
db = connect(":memory:")
db.execute("CREATE TABLE measurements (side REAL)")
db.execute("INSERT INTO measurements VALUES (5.0)")

# Can create functional indexes with deterministic functions
db.execute("CREATE INDEX idx_area ON measurements (SQUARE(side))")
result = db.query("SELECT SQUARE(side) as area FROM measurements")
print(result)  # [{"area": 25.0}]
```

### Variadic Function

```python
load("sqlite", "connect", "register_function")

# Register a function that accepts variable arguments
def greatest(*args):
    valid_args = [arg for arg in args if arg is not None]
    return max(valid_args) if valid_args else None

register_function("GREATEST", greatest)  # num_args=None (default, means variadic)

# Use with any number of arguments
db = connect(":memory:")
result = db.query("SELECT GREATEST(1, 5, 3, 9, 2) as max_val")
print(result)  # [{"max_val": 9}]
```

### Multiple Database Connections

```python
load("sqlite", "connect", "register_function")

# Register functions once
register_function("DOUBLE", lambda x: x * 2, num_args=1)
register_function("CONCAT_WS", lambda sep, *args: sep.join([str(arg) for arg in args if arg is not None]))

# Functions are available to ALL connections opened after registration
db1 = connect(":memory:")
db2 = connect("app.db")

# Both databases can use the registered functions
db1.execute("CREATE TABLE test1 (val INTEGER)")
db1.execute("INSERT INTO test1 VALUES (5)")
result1 = db1.query("SELECT DOUBLE(val) FROM test1")

db2.execute("CREATE TABLE test2 (first TEXT, last TEXT)")
db2.execute("INSERT INTO test2 VALUES ('John', 'Doe')")
result2 = db2.query("SELECT CONCAT_WS(' ', first, last) as fullname FROM test2")
```

## Implementation Architecture

### Core Components

1. **Global Function Registry**: Package-wide registry of registered functions
2. **Type Conversion Bridge**: Converts between Starlark values and SQLite driver values
3. **Go Function Wrappers**: Bridge functions that adapt Starlark functions to Go function signatures
4. **Driver Integration**: Registration with `modernc.org/sqlite` package functions
5. **Thread Management**: Safe execution of Starlark functions in SQLite context

### Package-Wide Registration Flow

```
Starlark register_function() → Global Registry → Go Wrapper Creation → modernc.org/sqlite Registration → Available to ALL future connections
```

### Data Flow During Function Execution

```
SQL Query → SQLite Engine → Go Wrapper → Type Conversion → Starlark Function → Type Conversion → Go Wrapper → SQLite Engine → Result
```

### Type Mapping

| Starlark Type | SQLite Type | Go driver.Value | Notes |
|---------------|-------------|-----------------|-------|
| None          | NULL        | nil             |       |
| Bool          | INTEGER     | bool            |       |
| Int           | INTEGER     | int64           |       |
| Float         | REAL        | float64         |       |
| String        | TEXT        | string          | **Special case**: Datetime strings |
| Bytes/String  | BLOB        | []byte          |       |
| Time          | TEXT        | time.Time       | **Special case**: Formatted as string |

#### Special Case: DateTime Handling

SQLite has special handling for datetime values that affects custom function behavior:

**String to DateTime Parsing:**

- When a column is declared with type `DATE`, `DATETIME`, or `TIMESTAMP`, SQLite automatically attempts to parse TEXT values as datetime strings
- Supported formats include ISO 8601 variants: `2006-01-02`, `2006-01-02T15:04:05`, `2006-01-02 15:04:05.999999999-07:00`, etc.
- If parsing succeeds, the value is converted to a `time.Time`; if it fails, it remains a string

**DateTime to String Conversion:**

- When a `time.Time` value is passed to SQLite (e.g., as a function parameter), it's automatically converted to a string using the connection's time format
- Default format: Go's `time.Time.String()` format
- Can be configured using `_time_format=sqlite` connection parameter for ISO 8601 format

**Implications for Custom Functions:**

```python
# Function receives string if column isn't declared as datetime type
register_function("PROCESS_DATE", lambda date_str: parse_and_format(date_str))

# Function receives time.Time if column is declared as DATETIME/DATE/TIMESTAMP
register_function("EXTRACT_YEAR", lambda dt: dt.year if dt else None)

# Function must handle both cases for robustness
def flexible_date_func(value):
    if isinstance(value, str):
        # Try to parse string as datetime
        try:
            dt = parse_datetime_string(value)
            return dt.year
        except:
            return None
    elif hasattr(value, 'year'):  # time.Time-like object
        return value.year
    else:
        return None

register_function("GET_YEAR", flexible_date_func)
```

### Error Handling

- **Registration Errors**: Invalid function names, signature mismatches, duplicate registrations
- **Runtime Errors**: Type conversion failures, Starlark execution errors
- **SQL Integration**: Errors are propagated as SQL errors with descriptive messages

## Technical Implementation

### Global Registry Structure

```go
// Global registry for custom functions
var (
    registeredFuncs = make(map[string]*registeredFunction)
    funcMutex       sync.RWMutex
)

type registeredFunction struct {
    name           string
    starlarkFunc   starlark.Callable
    numArgs        int32  // -1 for variadic
    deterministic  bool
}
```

### Function Registration Process

1. **Validation**: Check function name uniqueness, callable validity, argument constraints
2. **Storage**: Store function metadata in global registry
3. **Wrapper Creation**: Create Go function that bridges to Starlark
4. **Driver Registration**: Register with `modernc.org/sqlite` using appropriate function:
   - `sqlite.RegisterScalarFunction()` for non-deterministic functions
   - `sqlite.RegisterDeterministicScalarFunction()` for deterministic functions
5. **Availability**: Function becomes available to all future database connections

### Module Interface Enhancement

```go
// Add to module's StringDict
dict := starlark.StringDict{
    // ... existing functions ...
    "register_function": starlark.NewBuiltin("register_function", registerFunction),
}

// Global function registration (not per-database)
func registerFunction(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
    // Implementation details...
}
```

### Thread Safety and Execution

```go
// Go wrapper function for SQLite driver
func createGoFunctionWrapper(regFunc *registeredFunction) func(ctx *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
    return func(ctx *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
        // Create new thread for this function call
        thread := &starlark.Thread{
            Name: fmt.Sprintf("custom_function_%s", regFunc.name),
        }
        
        // Convert arguments, call Starlark function, convert result
        // ... implementation details ...
    }
}
```

### Registration Timing Requirements

Functions **MUST** be registered before opening database connections:

```python
# CORRECT: Register before opening connections
load("sqlite", "connect", "register_function")

register_function("MY_FUNC", lambda x: x * 2)  # Register first
db = connect("database.db")                     # Then open connection

# INCORRECT: Register after opening connection
load("sqlite", "connect", "register_function")

db = connect("database.db")                     # Open connection first
register_function("MY_FUNC", lambda x: x * 2)  # Too late! Function not available
```

## Performance Considerations

### Deterministic Functions

Functions marked as `deterministic=True` enable SQLite optimizations:

- **Result Caching**: SQLite can cache results for identical inputs
- **Constant Folding**: Evaluation at compile time for constant inputs
- **Functional Indexes**: Can create indexes on function results
- **Query Optimization**: Better query plan generation

Example:

```python
# Deterministic function - results can be cached
register_function("SQUARE", lambda x: x * x, num_args=1, deterministic=True)

# Non-deterministic function - results cannot be cached
register_function("RANDOM_HASH", lambda x: hash(x) + random.randint(1, 1000), num_args=1, deterministic=False)
```

### Memory Management

- Global function registry persists for application lifetime
- Each function execution gets its own Starlark thread context
- Efficient type conversion with minimal allocations
- No per-database cleanup needed (functions are global)

### Thread Safety

- Global registry protected by read-write mutex
- Function execution is thread-safe (each call gets new Starlark thread)
- No shared state between function calls

## Security Considerations

### Sandboxing

- Starlark's inherent sandboxing provides safety
- No file system or network access from functions
- Deterministic execution model prevents system interference

### Resource Limits

- Leverage Starlark's built-in protection against infinite loops
- Memory usage bounded by Starlark runtime limits
- Function execution time naturally limited by SQL query timeouts

### Function Isolation

- Each function execution runs in isolated Starlark thread
- No shared global state between function calls
- No access to database connection from within functions

## Testing Strategy

### Unit Tests

```go
func TestFunctionRegistration(t *testing.T) {
    // Test basic registration
    // Test duplicate registration handling
    // Test invalid parameters
    // Test function name validation
}

func TestTypeConversion(t *testing.T) {
    // Test all supported type conversions
    // Test edge cases (nil, large numbers, unicode strings)
    // Test error conditions
}

func TestThreadSafety(t *testing.T) {
    // Test concurrent function registration
    // Test concurrent function execution
    // Test registry access patterns
}
```

### Integration Tests

```python
def test_basic_function():
    load("sqlite", "connect", "register_function")
    
    register_function("DOUBLE", lambda x: x * 2, num_args=1)
    
    db = connect(":memory:")
    db.execute("CREATE TABLE test (val INTEGER)")
    db.execute("INSERT INTO test VALUES (5)")
    
    result = db.query("SELECT DOUBLE(val) as doubled FROM test")
    assert result[0]["doubled"] == 10

def test_deterministic_function():
    load("sqlite", "connect", "register_function")
    
    register_function("SQUARE", lambda x: x * x, num_args=1, deterministic=True)
    
    db = connect(":memory:")
    db.execute("CREATE TABLE test (val INTEGER)")
    db.execute("INSERT INTO test VALUES (5)")
    
    # Test that deterministic function can be used in index
    db.execute("CREATE INDEX idx_square ON test (SQUARE(val))")
    result = db.query("SELECT SQUARE(val) as squared FROM test")
    assert result[0]["squared"] == 25

def test_variadic_function():
    load("sqlite", "connect", "register_function")
    
    def concat_all(*args):
        return "|".join([str(arg) for arg in args if arg is not None])
    
    register_function("CONCAT_ALL", concat_all)
    
    db = connect(":memory:")
    result = db.query("SELECT CONCAT_ALL('a', 'b', 'c') as result")
    assert result[0]["result"] == "a|b|c"

def test_error_handling():
    load("sqlite", "connect", "register_function")
    
    # Test invalid function registration
    try:
        register_function("", lambda x: x)  # Empty name
        assert False, "Should have raised error"
    except Exception as e:
        assert "function name cannot be empty" in str(e)
    
    # Test duplicate registration
    register_function("TEST_FUNC", lambda x: x)
    try:
        register_function("TEST_FUNC", lambda x: x * 2)  # Duplicate
        assert False, "Should have raised error"
    except Exception as e:
        assert "already registered" in str(e)

def test_multiple_connections():
    load("sqlite", "connect", "register_function")
    
    register_function("ADD_ONE", lambda x: x + 1, num_args=1)
    
    # Both connections should have access to the function
    db1 = connect(":memory:")
    db2 = connect(":memory:")
    
    db1.execute("CREATE TABLE test1 (val INTEGER)")
    db1.execute("INSERT INTO test1 VALUES (5)")
    
    db2.execute("CREATE TABLE test2 (val INTEGER)")
    db2.execute("INSERT INTO test2 VALUES (10)")
    
    result1 = db1.query("SELECT ADD_ONE(val) as result FROM test1")
    result2 = db2.query("SELECT ADD_ONE(val) as result FROM test2")
    
    assert result1[0]["result"] == 6
    assert result2[0]["result"] == 11

def test_datetime_handling():
    load("sqlite", "connect", "register_function")
    
    # Register functions that work with datetime values
    register_function("EXTRACT_YEAR", lambda dt: dt.year if hasattr(dt, 'year') else None, num_args=1)
    register_function("FORMAT_DATE", lambda dt: "{}-{:02d}-{:02d}".format(dt.year, dt.month, dt.day) if hasattr(dt, 'year') else str(dt), num_args=1)
    
    # Register a function that handles both string and datetime inputs
    def get_year_flexible(value):
        if hasattr(value, 'year'):  # time.Time object
            return value.year
        elif isinstance(value, str):
            # Try to parse as year from string (simple case)
            try:
                if len(value) >= 4 and value[:4].isdigit():
                    return int(value[:4])
            except:
                pass
        return None
    
    register_function("GET_YEAR", get_year_flexible, num_args=1)
    
    db = connect(":memory:")
    
    # Test with datetime column type
    db.execute("CREATE TABLE events (id INTEGER, event_date DATETIME, name TEXT)")
    db.execute("INSERT INTO events VALUES (1, '2023-06-15 14:30:00', 'Meeting')")
    db.execute("INSERT INTO events VALUES (2, '2024-01-01', 'New Year')")
    
    # Test with text column type (no automatic datetime parsing)
    db.execute("CREATE TABLE logs (id INTEGER, timestamp TEXT)")
    db.execute("INSERT INTO logs VALUES (1, '2023-06-15 14:30:00')")
    
    # Test datetime functions on DATETIME column
    result1 = db.query("SELECT EXTRACT_YEAR(event_date) as year FROM events WHERE id = 1")
    assert result1[0]["year"] == 2023
    
    result2 = db.query("SELECT FORMAT_DATE(event_date) as formatted FROM events WHERE id = 2")
    assert result2[0]["formatted"] == "2024-01-01"
    
    # Test flexible function on both column types
    result3 = db.query("SELECT GET_YEAR(event_date) as year FROM events WHERE id = 1")
    assert result3[0]["year"] == 2023
    
    result4 = db.query("SELECT GET_YEAR(timestamp) as year FROM logs WHERE id = 1") 
    assert result4[0]["year"] == 2023
```

## Future Enhancements

### Aggregate Functions

Support for custom aggregate functions using `sqlite.FunctionImpl`:

```python
sqlite.register_aggregate("MY_AVG", MyAverageAggregator, num_args=1)
```

### Function Unregistration

Allow unregistering functions (if supported by driver):

```python
sqlite.unregister_function("FUNCTION_NAME")
```

### Function Introspection

Query registered functions:

```python
functions = sqlite.list_registered_functions()
```

### Function Documentation

Attach documentation to registered functions:

```python
sqlite.register_function("SQUARE", lambda x: x * x, 
                        num_args=1, 
                        deterministic=True,
                        doc="Returns the square of a number")
```

## Compatibility

- **Go Version**: 1.18+
- **SQLite Version**: Any version supported by modernc.org/sqlite
- **Starlark Version**: Compatible with go.starlark.net
- **Platform Support**: All platforms supported by the base module
- **Driver Functions**: Uses `sqlite.RegisterScalarFunction` and `sqlite.RegisterDeterministicScalarFunction`

## Migration Notes

This is a new feature with no breaking changes to existing APIs. All existing database operations continue to work unchanged.

### Key Architectural Changes from Initial Design

1. **Function registration moved from database instance to module level**
2. **Global function registry instead of per-database registry**
3. **Registration must happen before opening connections**
4. **Functions are available to all connections opened after registration**
5. **No cleanup needed on database close (functions are global)**

### Best Practices

1. **Register all functions at application startup** before opening any database connections
2. **Use descriptive function names** to avoid conflicts with SQLite built-ins
3. **Mark mathematical/pure functions as deterministic** for optimization benefits
4. **Handle None values gracefully** in function implementations
5. **Keep functions simple** - complex logic should be done outside the SQL function
6. **Handle datetime values properly** - functions may receive either strings or time.Time objects depending on column type declarations
7. **Consider column type declarations** when designing datetime-handling functions - `DATETIME`/`DATE`/`TIMESTAMP` columns auto-parse strings to time.Time objects
8. **Test with different column types** to ensure functions work with both string and parsed datetime inputs
