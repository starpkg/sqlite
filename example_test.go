package sqlite

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/1set/starlet"
	"github.com/starpkg/base"
	"go.starlark.net/starlark"
)

// TestStarlarkScripts runs Starlark test scripts from the test directory.
// Scripts with "test-" prefix should succeed, "panic-" prefix should fail.
func TestStarlarkScripts(t *testing.T) {
	scriptDir := filepath.Join("..", "test", ModuleName)
	runDir := filepath.Join(t.TempDir(), ModuleName)
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatalf("failed to create Starlark test run directory: %v", err)
	}

	scripts, err := filepath.Glob(filepath.Join(scriptDir, "*.star"))
	if err != nil {
		t.Fatalf("failed to list Starlark tests: %v", err)
	}
	for _, scriptPath := range scripts {
		content, err := os.ReadFile(scriptPath)
		if err != nil {
			t.Fatalf("failed to read %q: %v", scriptPath, err)
		}
		dst := filepath.Join(runDir, filepath.Base(scriptPath))
		if err := os.WriteFile(dst, content, 0o644); err != nil {
			t.Fatalf("failed to copy %q: %v", scriptPath, err)
		}
	}

	// Create a module factory function that returns a fresh module loader for each test
	moduleFactory := func() starlet.ModuleLoader {
		return NewModule().LoadModule()
	}
	extraModules := []string{"go_idiomatic"}

	// Use the helper function from the base package
	base.RunStarlarkTests(t, ModuleName, moduleFactory, extraModules, runDir)
}

func testConfigOption[T any](name, description string, value T) *base.ConfigOption[T] {
	return genConfigOption(name, description, value).WithValue(value)
}

func newTestModuleWithConfig(database string, maxRows int, restrictFileAccess bool) *Module {
	m := newModuleWithOptions(
		testConfigOption(configKeyDatabase, "Path to SQLite database (use :memory: for in-memory)", database),
		testConfigOption(configKeyTimeout, "Connection timeout in seconds", defaultTimeout),
		testConfigOption(configKeyBusyTimeout, "Busy timeout in seconds", defaultBusyTimeout),
		testConfigOption(configKeyForeignKeys, "Enable foreign key constraints", defaultForeignKeys),
		testConfigOption(configKeyJournalMode, "Journal mode (WAL, DELETE, TRUNCATE, PERSIST, MEMORY, OFF)", defaultJournalMode),
		testConfigOption(configKeySynchronous, "Synchronous mode (FULL, NORMAL, OFF)", defaultSynchronous),
		testConfigOption(configKeyCacheSize, "Cache size in number of pages", defaultCacheSize),
		testConfigOption(configKeyMaxRows, "Maximum rows returned by query helpers (0 means unlimited)", maxRows),
	)
	m.restrictFileAccess = restrictFileAccess
	return m
}

func runSQLiteScript(t *testing.T, script string, moduleFactory func() starlet.ModuleLoader) error {
	t.Helper()

	s := starlet.NewDefault()
	s.AddLazyloadModules(map[string]starlet.ModuleLoader{
		ModuleName: moduleFactory(),
	})
	_, err := s.RunScript([]byte(script), nil)
	return err
}

func requireSQLiteScript(t *testing.T, script string, moduleFactory func() starlet.ModuleLoader) {
	t.Helper()

	if err := runSQLiteScript(t, script, moduleFactory); err != nil {
		t.Fatalf("script failed: %v", err)
	}
}

func requireSQLiteScriptErrorContains(t *testing.T, script string, moduleFactory func() starlet.ModuleLoader, want string) {
	t.Helper()

	err := runSQLiteScript(t, script, moduleFactory)
	if err == nil {
		t.Fatalf("expected script error containing %q", want)
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("expected script error containing %q, got %v", want, err)
	}
}

func quoteStarlarkString(s string) string {
	return fmt.Sprintf("%q", s)
}

func TestMaxRows(t *testing.T) {
	const unrestrictedScript = `
load("sqlite", "connect")

def main():
    db = connect(":memory:")
    db.execute("CREATE TABLE rows (id INTEGER PRIMARY KEY)")
    for i in range(10):
        db.execute("INSERT INTO rows (id) VALUES (?)", [i])

    queried = db.query("SELECT id FROM rows ORDER BY id")
    if len(queried) != 10:
        fail("expected 10 queried rows, got {}".format(len(queried)))

    selected = db.select("rows", ["id"], order_by="id")
    if len(selected) != 10:
        fail("expected 10 selected rows, got {}".format(len(selected)))

    stmt = db.prepare_query("SELECT id FROM rows ORDER BY id")
    prepared = stmt.query()
    if len(prepared) != 10:
        fail("expected 10 prepared rows, got {}".format(len(prepared)))
    stmt.close()

    tx = db.begin()
    tx_result = tx.query("SELECT id FROM rows ORDER BY id")
    if not tx_result.ok:
        fail("transaction query failed: {}".format(tx_result.error))
    if len(tx_result.value) != 10:
        fail("expected 10 transaction rows, got {}".format(len(tx_result.value)))
    tx.rollback()

    db.close()

main()
`

	requireSQLiteScript(t, unrestrictedScript, func() starlet.ModuleLoader {
		return newTestModuleWithConfig(defaultDatabase, 0, false).LoadModule()
	})
}

// TestMaxRowsAtLimit verifies a result set exactly at the limit is allowed.
func TestMaxRowsAtLimit(t *testing.T) {
	const atLimitScript = `
load("sqlite", "connect")

def main():
    db = connect(":memory:")
    db.execute("CREATE TABLE rows (id INTEGER PRIMARY KEY)")
    for i in range(3):
        db.execute("INSERT INTO rows (id) VALUES (?)", [i])

    if len(db.query("SELECT id FROM rows ORDER BY id")) != 3:
        fail("db.query should allow rows at the limit")
    if len(db.select("rows", ["id"], order_by="id")) != 3:
        fail("db.select should allow rows at the limit")

    stmt = db.prepare_query("SELECT id FROM rows ORDER BY id")
    if len(stmt.query()) != 3:
        fail("prepared query should allow rows at the limit")
    stmt.close()

    tx = db.begin()
    tx_result = tx.query("SELECT id FROM rows ORDER BY id")
    if not tx_result.ok:
        fail("transaction query should allow rows at the limit: {}".format(tx_result.error))
    if len(tx_result.value) != 3:
        fail("transaction query should return rows at the limit")
    tx.rollback()

    db.close()

main()
`

	requireSQLiteScript(t, atLimitScript, func() starlet.ModuleLoader {
		return newTestModuleWithConfig(defaultDatabase, 3, false).LoadModule()
	})
}

// TestMaxRowsDBQueryExceeds verifies db.query past the limit returns an error.
func TestMaxRowsDBQueryExceeds(t *testing.T) {
	const exceedsQueryScript = `
load("sqlite", "connect")

def main():
    db = connect(":memory:")
    db.execute("CREATE TABLE rows (id INTEGER PRIMARY KEY)")
    for i in range(3):
        db.execute("INSERT INTO rows (id) VALUES (?)", [i])
    db.query("SELECT id FROM rows ORDER BY id")

main()
`

	requireSQLiteScriptErrorContains(t, exceedsQueryScript, func() starlet.ModuleLoader {
		return newTestModuleWithConfig(defaultDatabase, 2, false).LoadModule()
	}, "query result exceeds max_rows limit (2)")
}

// TestMaxRowsTransactionExceeds verifies a tx query past the limit surfaces an error result.
func TestMaxRowsTransactionExceeds(t *testing.T) {
	const exceedsTransactionScript = `
load("sqlite", "connect")

def main():
    db = connect(":memory:")
    db.execute("CREATE TABLE rows (id INTEGER PRIMARY KEY)")
    for i in range(3):
        db.execute("INSERT INTO rows (id) VALUES (?)", [i])

    tx = db.begin()
    tx_result = tx.query("SELECT id FROM rows ORDER BY id")
    if tx_result.ok:
        fail("transaction query should have failed")
    if "query result exceeds max_rows limit (2)" not in tx_result.error:
        fail("unexpected transaction query error: {}".format(tx_result.error))
    tx.rollback()
    db.close()

main()
`

	requireSQLiteScript(t, exceedsTransactionScript, func() starlet.ModuleLoader {
		return newTestModuleWithConfig(defaultDatabase, 2, false).LoadModule()
	})
}

func TestFileAccessRestriction(t *testing.T) {
	restrictedErr := "file database access is restricted by the host; only in-memory or the host-configured database is allowed"

	t.Run("default allows file database", func(t *testing.T) {
		dbPath := filepath.Join(t.TempDir(), "default.db")
		script := fmt.Sprintf(`
load("sqlite", "connect")

def main():
    db = connect(database=%s)
    db.execute("CREATE TABLE t (id INTEGER)")
    db.close()

main()
`, quoteStarlarkString(dbPath))

		requireSQLiteScript(t, script, func() starlet.ModuleLoader {
			return NewModule().LoadModule()
		})
	})

	t.Run("restricted rejects file database", func(t *testing.T) {
		dbPath := filepath.Join(t.TempDir(), "restricted.db")
		script := fmt.Sprintf(`
load("sqlite", "connect")

def main():
    connect(database=%s)

main()
`, quoteStarlarkString(dbPath))

		requireSQLiteScriptErrorContains(t, script, func() starlet.ModuleLoader {
			return NewModuleWithFileAccess(false).LoadModule()
		}, restrictedErr)
	})

	t.Run("restricted allows memory database", func(t *testing.T) {
		const script = `
load("sqlite", "connect")

def main():
    db = connect(":memory:")
    db.execute("CREATE TABLE t (id INTEGER)")
    db.close()

main()
`

		requireSQLiteScript(t, script, func() starlet.ModuleLoader {
			return NewModuleWithFileAccess(false).LoadModule()
		})
	})

	t.Run("restricted allows host configured database", func(t *testing.T) {
		dbPath := filepath.Join(t.TempDir(), "host.db")
		t.Setenv("SQLITE_DATABASE", dbPath)
		const script = `
load("sqlite", "connect")

def main():
    db = connect()
    db.execute("CREATE TABLE t (id INTEGER)")
    db.close()

main()
`

		requireSQLiteScript(t, script, func() starlet.ModuleLoader {
			return NewModuleWithFileAccess(false).LoadModule()
		})
	})

	t.Run("restricted rejects file attach", func(t *testing.T) {
		dbPath := filepath.Join(t.TempDir(), "attached.db")
		script := fmt.Sprintf(`
load("sqlite", "connect")

def main():
    db = connect(":memory:")
    db.attach(%s, "attached")

main()
`, quoteStarlarkString(dbPath))

		requireSQLiteScriptErrorContains(t, script, func() starlet.ModuleLoader {
			return NewModuleWithFileAccess(false).LoadModule()
		}, "file database attach is restricted by the host; only in-memory databases are allowed")
	})
}

func TestPanickingRegisteredFunctionReturnsError(t *testing.T) {
	regFunc := &registeredFunction{
		name: "TEST_PANIC",
		starlarkFunc: starlark.NewBuiltin("TEST_PANIC", func(_ *starlark.Thread, _ *starlark.Builtin, _ starlark.Tuple, _ []starlark.Tuple) (starlark.Value, error) {
			panic("boom")
		}),
	}

	wrapper := createGoFunctionWrapper(regFunc)
	_, err := wrapper(nil, nil)
	if err == nil {
		t.Fatal("expected panic to be returned as an error")
	}
	if !strings.Contains(err.Error(), `custom function "TEST_PANIC" panicked: boom`) {
		t.Fatalf("unexpected panic error: %v", err)
	}
}

func TestExamples(t *testing.T) {
	tests := []struct {
		name   string
		script string
	}{
		{"BasicExample", `
# Load the sqlite module
load("sqlite", "connect")

def main():
    # Connect to an in-memory database with timeout configuration
    db = connect(":memory:", timeout=30.0, busy_timeout=5.0)

    # Create a users table
    db.execute("""
        CREATE TABLE users (
            id INTEGER PRIMARY KEY,
            name TEXT NOT NULL,
            email TEXT UNIQUE,
            age INTEGER
        )
    """)

    # Insert some data
    db.execute("INSERT INTO users (name, email, age) VALUES (?, ?, ?)", ["Alice", "alice@example.com", 30])
    db.execute("INSERT INTO users (name, email, age) VALUES (?, ?, ?)", ["Bob", "bob@example.com", 25])
    
    # Query data
    rows = db.query("SELECT * FROM users ORDER BY age DESC")
    for row in rows:
        print("Name: {}, Age: {}".format(row["name"], row["age"]))
    
    # Prepared statement
    stmt = db.prepare("INSERT INTO users (name, email, age) VALUES (?, ?, ?)")
    stmt.execute(["Charlie", "charlie@example.com", 35])
    stmt.close()
    
    # Transaction
    tx = db.begin()
    update_result = tx.execute("UPDATE users SET age = age + 1 WHERE name = ?", ["Alice"])
    if update_result.ok:
        commit_result = tx.commit()
        if not commit_result.ok:
            fail("Failed to commit transaction: {}".format(commit_result.error))
    else:
        fail("Failed to update user age: {}".format(update_result.error))
    
    # Query with parameters
    user = db.query_one("SELECT * FROM users WHERE name = ?", ["Alice"])
    if user:
        print("Alice's new age: {}".format(user["age"]))
        # Verify age was incremented properly
        if user["age"] != 31:
            fail("Expected Alice's age to be 31, but got {}".format(user["age"]))
    else:
        fail("Failed to find Alice in database")
    
    # High-level table operations
    db.create_table("products", {
        "id": "INTEGER PRIMARY KEY",
        "name": "TEXT NOT NULL",
        "price": "REAL DEFAULT 0.0"
    })
    
    # Insert records
    product_id = db.insert("products", {"name": "Laptop", "price": 999.99})
    print("Inserted product with ID: {}".format(product_id))
    
    # Query the table
    rows = db.query("SELECT * FROM products")
    for row in rows:
        print("Product: {}, Price: {}".format(row["name"], row["price"]))
        # Verify product was inserted correctly
        if row["name"] != "Laptop" or row["price"] != 999.99:
            fail("Product values don't match: expected Laptop/999.99, got {}/{}".format(
                row["name"], row["price"]))
    
    # Verify the row count
    count = db.count("products")
    if count != 1:
        fail("Expected 1 product, found {}".format(count))
    
    # Close the connection
    db.close()
    
    print("✓ All verifications passed")

main()
`},
		{"TransactionErrorHandling", `
load("sqlite", "connect")

def main():
    """Test the new OperationResult-based transaction error handling."""
    print("Testing OperationResult-based transaction error handling...")
    
    # Connect to an in-memory database
    db = connect(":memory:")
    
    # Create test table
    db.execute("""
        CREATE TABLE accounts (
            id INTEGER PRIMARY KEY,
            name TEXT NOT NULL UNIQUE,
            balance REAL NOT NULL DEFAULT 0.0
        )
    """)
    
    # Insert test data
    db.execute("INSERT INTO accounts (id, name, balance) VALUES (1, 'Alice', 1000.0)")
    db.execute("INSERT INTO accounts (id, name, balance) VALUES (2, 'Bob', 500.0)") 
    
    # Test 1: Successful transaction operations
    print("Test 1: Successful operations")
    tx = db.begin()
    
    # Test successful execute
    debit_result = tx.execute("UPDATE accounts SET balance = balance - ? WHERE id = ?", [100.0, 1])
    if not debit_result.ok:
        fail("Execute should succeed: {}".format(debit_result.error))
    if debit_result.value != 1:  # 1 row affected
        fail("Expected 1 row affected, got {}".format(debit_result.value))
    print("✓ Execute result: ok={}, value={}".format(debit_result.ok, debit_result.value))
    
    # Test successful query
    balance_result = tx.query("SELECT * FROM accounts WHERE id = 1")
    if not balance_result.ok:
        fail("Query should succeed: {}".format(balance_result.error))
    if len(balance_result.value) != 1:
        fail("Expected 1 result row")
    print("✓ Query result: ok={}, rows={}".format(balance_result.ok, len(balance_result.value)))
    
    # Test successful query_one
    one_result = tx.query_one("SELECT balance FROM accounts WHERE id = 1")
    if not one_result.ok:
        fail("Query_one should succeed: {}".format(one_result.error))
    if one_result.value["balance"] != 900.0:
        fail("Expected balance to be 900.0, got {}".format(one_result.value["balance"]))
    print("✓ Query_one result: ok={}, balance={}".format(one_result.ok, one_result.value["balance"]))
    
    # Test successful commit
    commit_result = tx.commit()
    if not commit_result.ok:
        fail("Commit should succeed: {}".format(commit_result.error))
    print("✓ Commit result: ok={}".format(commit_result.ok))
    
    # Test 2: Error handling - SQL error
    print("Test 2: SQL error handling")
    tx2 = db.begin()
    
    # Try to execute invalid SQL
    bad_result = tx2.execute("INVALID SQL STATEMENT")
    if bad_result.ok:
        fail("Invalid SQL should fail")
    if bad_result.error == "":
        fail("Error message should not be empty")
    print("✓ SQL error caught: {}".format(bad_result.error))
    
    # Transaction should still be usable after error
    good_result = tx2.execute("SELECT COUNT(*) as count FROM accounts")
    if not good_result.ok:
        fail("Transaction should still be usable after SQL error")
    
    tx2.rollback()  # Clean up
    
    # Test 3: Error handling - constraint violation  
    print("Test 3: Constraint violation")
    tx3 = db.begin()
    
    # Try to insert duplicate unique key
    dup_result = tx3.execute("INSERT INTO accounts (id, name, balance) VALUES (3, 'Alice', 100.0)")
    if dup_result.ok:
        fail("Duplicate name should violate unique constraint")
    if "UNIQUE constraint failed" not in dup_result.error:
        fail("Expected UNIQUE constraint error, got: {}".format(dup_result.error))
    print("✓ Constraint error caught: {}".format(dup_result.error))
    
    tx3.rollback()
    
    # Test 4: No rows found (not an error)
    print("Test 4: No rows scenarios")
    tx4 = db.begin()
    
    # Query with no results
    empty_query = tx4.query("SELECT * FROM accounts WHERE id = 999")
    if not empty_query.ok:
        fail("Query with no results should succeed with empty list")
    if len(empty_query.value) != 0:
        fail("Expected empty result list")
    print("✓ Empty query result: ok={}, rows={}".format(empty_query.ok, len(empty_query.value)))
    
    # Query_one with no results
    empty_one = tx4.query_one("SELECT * FROM accounts WHERE id = 999") 
    if not empty_one.ok:
        fail("Query_one with no results should succeed with None")
    if empty_one.value != None:
        fail("Expected None result")
    print("✓ Empty query_one result: ok={}, value={}".format(empty_one.ok, empty_one.value))
    
    tx4.commit()
    
    # Test 5: Rollback still fails on error (as designed)
    print("Test 5: Rollback behavior")
    tx5 = db.begin() 
    tx5.execute("INSERT INTO accounts (id, name, balance) VALUES (3, 'Charlie', 200.0)")
    
    # Normal rollback should work
    tx5.rollback()
    print("✓ Normal rollback works")
    
    # Verify data was rolled back
    charlie = db.query_one("SELECT * FROM accounts WHERE name = 'Charlie'")
    if charlie != None:
        fail("Charlie should not exist after rollback")
    
    db.close()
    print("✓ All OperationResult tests passed")

main()
`},
		{"Transactions", `
load("sqlite", "connect")

def main():
    # Connect to an in-memory database
    db = connect(":memory:")

    # Set up test data
    db.execute("""
        CREATE TABLE accounts (
            id INTEGER PRIMARY KEY,
            name TEXT NOT NULL,
            balance REAL NOT NULL DEFAULT 0.0
        )
    """)
    
    # Insert initial accounts
    db.insert("accounts", {"id": 1, "name": "Alice", "balance": 1000.0})
    db.insert("accounts", {"id": 2, "name": "Bob", "balance": 500.0})
    
    # Verify initial balances
    alice_initial = db.query_one("SELECT balance FROM accounts WHERE id = 1")
    bob_initial = db.query_one("SELECT balance FROM accounts WHERE id = 2")
    
    if alice_initial["balance"] != 1000.0:
        fail("Initial Alice balance incorrect: {}".format(alice_initial["balance"]))
    if bob_initial["balance"] != 500.0:
        fail("Initial Bob balance incorrect: {}".format(bob_initial["balance"]))
    
    # Function that transfers money using a transaction
    def transfer_money(from_id, to_id, amount):
        tx = db.begin()
        
        # Check if from_account has sufficient funds
        from_account_result = tx.query_one("SELECT * FROM accounts WHERE id = ?", [from_id])
        if not from_account_result.ok:
            print("Failed to check account: {}".format(from_account_result.error))
            tx.rollback()
            return False
        
        from_account = from_account_result.value
        if not from_account or from_account["balance"] < amount:
            print("Insufficient funds, rolling back")
            tx.rollback()
            return False
        
        # Update balances
        debit_result = tx.execute("UPDATE accounts SET balance = balance - ? WHERE id = ?", [amount, from_id])
        if not debit_result.ok:
            print("Failed to debit account: {}".format(debit_result.error))
            tx.rollback()
            return False
        
        credit_result = tx.execute("UPDATE accounts SET balance = balance + ? WHERE id = ?", [amount, to_id])
        if not credit_result.ok:
            print("Failed to credit account: {}".format(credit_result.error))
            tx.rollback()
            return False
        
        # Commit the transaction
        commit_result = tx.commit()
        if not commit_result.ok:
            print("Failed to commit transaction: {}".format(commit_result.error))
            return False
        
        print("Transfer successful")
        return True
    
    # Successful transfer
    transfer_result = transfer_money(1, 2, 200.0)
    if not transfer_result:
        fail("Expected successful transfer")
    
    # Verify balances after successful transfer
    alice = db.query_one("SELECT * FROM accounts WHERE id = 1")
    bob = db.query_one("SELECT * FROM accounts WHERE id = 2")
    print("Alice balance: {}, Bob balance: {}".format(alice["balance"], bob["balance"]))
    
    # Verify exact amounts
    if alice["balance"] != 800.0:
        fail("Expected Alice balance to be 800.0, got {}".format(alice["balance"]))
    if bob["balance"] != 700.0:
        fail("Expected Bob balance to be 700.0, got {}".format(bob["balance"]))
    
    # Failed transfer (insufficient funds)
    failed_result = transfer_money(2, 1, 1000.0)
    if failed_result:
        fail("Transfer should have failed due to insufficient funds")
    
    # Verify balances unchanged after failed transfer
    alice_after_fail = db.query_one("SELECT balance FROM accounts WHERE id = 1")
    bob_after_fail = db.query_one("SELECT balance FROM accounts WHERE id = 2")
    
    if alice_after_fail["balance"] != 800.0:
        fail("Alice balance should be unchanged at 800.0")
    if bob_after_fail["balance"] != 700.0:
        fail("Bob balance should be unchanged at 700.0")
    
    # Close the connection
    db.close()
    
    print("✓ All transaction tests passed")

main()
`},
		{"PreparedStatements", `
load("sqlite", "connect")

def main():
    # Connect to an in-memory database
    db = connect(":memory:")

    # Create a test table
    db.execute("""
        CREATE TABLE measurements (
            id INTEGER PRIMARY KEY,
            sensor_id INTEGER NOT NULL,
            temperature REAL NOT NULL,
            humidity REAL,
            timestamp TEXT DEFAULT CURRENT_TIMESTAMP
        )
    """)
    
    # Create a prepared statement for inserting data
    insert_stmt = db.prepare("INSERT INTO measurements (sensor_id, temperature, humidity) VALUES (?, ?, ?)")
    
    # Insert multiple records efficiently using the prepared statement
    sensor_data = [
        [1, 22.5, 45.2],
        [1, 22.8, 45.5],
        [2, 18.2, 50.0],
        [2, 18.5, 49.8],
        [3, 25.1, 30.5]
    ]
    
    for data in sensor_data:
        insert_stmt.execute(data)
    
    # Close the statement when done
    insert_stmt.close()
    
    # Verify the correct number of records were inserted
    count = db.count("measurements", "")
    if count != 5:
        fail("Expected 5 measurements, found {}".format(count))
    
    # Verify records by sensor
    sensor1_count = db.count("measurements", "sensor_id = 1")
    sensor2_count = db.count("measurements", "sensor_id = 2")
    sensor3_count = db.count("measurements", "sensor_id = 3")
    
    if sensor1_count != 2:
        fail("Expected 2 records for sensor 1, found {}".format(sensor1_count))
    if sensor2_count != 2:
        fail("Expected 2 records for sensor 2, found {}".format(sensor2_count))
    if sensor3_count != 1:
        fail("Expected 1 record for sensor 3, found {}".format(sensor3_count))
    
    # Create a prepared query statement
    query_stmt = db.prepare_query("SELECT * FROM measurements WHERE sensor_id = ? ORDER BY temperature DESC")
    
    # Use the prepared query multiple times with different parameters
    print("Sensor 1 measurements:")
    sensor1_rows = query_stmt.query([1])
    row_count = 0
    prev_temp = 999.9  # Ensure descending order
    
    for row in sensor1_rows:
        row_count += 1
        print("  Temperature: {}, Humidity: {}".format(row["temperature"], row["humidity"]))
        # Verify order and values
        if row["temperature"] > prev_temp:
            fail("Results not in descending temperature order")
        prev_temp = row["temperature"]
    
    if row_count != 2:
        fail("Expected 2 rows for sensor 1, got {}".format(row_count))
    
    # Similar check for sensor 2
    print("Sensor 2 measurements:")
    sensor2_rows = query_stmt.query([2])
    if len(sensor2_rows) != 2:
        fail("Expected 2 rows for sensor 2")
    
    # Close the prepared query
    query_stmt.close()
    
    # Test a different kind of prepared query
    max_temp_stmt = db.prepare_query("SELECT MAX(temperature) as max_temp FROM measurements WHERE sensor_id = ?")
    max_result = max_temp_stmt.query_one([1])
    if max_result["max_temp"] != 22.8:
        fail("Expected max temperature for sensor 1 to be 22.8, got {}".format(max_result["max_temp"]))
    max_temp_stmt.close()
    
    # Close the database connection
    db.close()
    
    print("✓ All prepared statement tests passed")

main()
`},
		{"HighLevelOperations", `
load("sqlite", "connect")

def main():
    # Connect to an in-memory database
    db = connect(":memory:")

    # Create a table using high-level API
    db.create_table("employees", {
        "id": "INTEGER PRIMARY KEY",
        "name": "TEXT NOT NULL",
        "department": "TEXT NOT NULL",
        "salary": "REAL DEFAULT 0.0",
        "hire_date": "TEXT DEFAULT CURRENT_DATE"
    })
    
    # Verify table was created
    if not db.table_exists("employees"):
        fail("employees table should exist")
    
    # Insert records using the high-level API
    id1 = db.insert("employees", {"name": "John Doe", "department": "Engineering", "salary": 85000})
    id2 = db.insert("employees", {"name": "Jane Smith", "department": "Marketing", "salary": 75000})
    
    # Verify IDs are as expected (SQLite should auto-assign 1, 2)
    if id1 != 1:
        fail("Expected first insert ID to be 1, got {}".format(id1))
    if id2 != 2:
        fail("Expected second insert ID to be 2, got {}".format(id2))
    
    # Bulk insert multiple records
    db.insert_many("employees", [
        {"name": "Bob Johnson", "department": "Engineering", "salary": 90000},
        {"name": "Alice Williams", "department": "HR", "salary": 65000},
        {"name": "Charlie Brown", "department": "Engineering", "salary": 80000}
    ])
    
    # Verify total record count
    total_count = db.count("employees", "")
    if total_count != 5:
        fail("Expected 5 total employees, found {}".format(total_count))
    
    # Count employees by department
    eng_count = db.count("employees", ["department = ?", "Engineering"])
    print("Engineering employees: {}".format(eng_count))
    if eng_count != 3:
        fail("Expected 3 Engineering employees, found {}".format(eng_count))
    
    # Select all employees from a specific department
    engineers = db.select("employees", ["name", "salary"], ["department = ?", "Engineering"], order_by="salary DESC")
    print("Engineering team:")
    
    # Verify engineers are returned in correct order (by descending salary)
    if len(engineers) != 3:
        fail("Expected 3 engineers, got {}".format(len(engineers)))
    
    expected_names = ["Bob Johnson", "John Doe", "Charlie Brown"]
    expected_salaries = [90000, 85000, 80000]
    
    for i, eng in enumerate(engineers):
        print("  {} - ${}".format(eng["name"], eng["salary"]))
        if eng["name"] != expected_names[i] or eng["salary"] != expected_salaries[i]:
            fail("Expected engineer {} to be {}, got {}".format(i, expected_names[i], eng["name"]))
    
    # Update records
    updated_rows = db.update("employees", {"salary": 95000}, ["name = ?", "Bob Johnson"])
    if updated_rows != 1:
        fail("Expected to update 1 row, updated {}".format(updated_rows))
    
    # Verify the update
    bob = db.query_one("SELECT * FROM employees WHERE name = ?", ["Bob Johnson"])
    if bob["salary"] != 95000:
        fail("Expected Bob's salary to be 95000, got {}".format(bob["salary"]))
    
    # Upsert (update or insert)
    db.upsert("employees", {"id": 1, "name": "John Doe", "department": "Engineering", "salary": 88000}, ["id"])
    
    # Verify the update
    john = db.query_one("SELECT * FROM employees WHERE name = ?", ["John Doe"])
    print("John's updated salary: ${}".format(john["salary"]))
    if john["salary"] != 88000:
        fail("Expected John's salary to be 88000, got {}".format(john["salary"]))
    
    # Check if a table exists
    if db.table_exists("employees"):
        print("Employees table exists")
    
    # Get table information
    columns = db.table_info("employees")
    expected_columns = ["id", "name", "department", "salary", "hire_date"]
    column_names = [col["name"] for col in columns]
    
    for col_name in expected_columns:
        if col_name not in column_names:
            fail("Expected column {} not found in table schema".format(col_name))
    
    print("Table columns:")
    for col in columns:
        print("  {} ({})".format(col["name"], col["type"]))
    
    # Delete a record
    deleted_rows = db.delete("employees", ["name = ?", "Alice Williams"])
    if deleted_rows != 1:
        fail("Expected to delete 1 row, deleted {}".format(deleted_rows))
    
    # Verify record was deleted
    alice = db.query_one("SELECT * FROM employees WHERE name = ?", ["Alice Williams"])
    if alice:
        fail("Alice should have been deleted but was found")
    
    # Verify count after deletion
    after_delete_count = db.count("employees", "")
    if after_delete_count != 4:
        fail("Expected 4 employees after deletion, found {}".format(after_delete_count))
    
    # List all tables
    tables = db.tables()
    print("Database tables: {}".format(tables))
    if "employees" not in tables:
        fail("employees should be in the list of tables")
    if len(tables) != 1:
        fail("Expected 1 table, found {}".format(len(tables)))
    
    # Close the connection
    db.close()
    
    print("✓ All high-level operation tests passed")

main()
`},
		{"BatchOperations", `
load("sqlite", "connect")

def main():
    # Connect to an in-memory database
    db = connect(":memory:")
    
    # Create tables using batch operations
    setup_results = db.batch([
        """CREATE TABLE accounts (
            id INTEGER PRIMARY KEY,
            name TEXT NOT NULL,
            balance REAL NOT NULL DEFAULT 0.0
        )""",
        """CREATE TABLE transactions (
            id INTEGER PRIMARY KEY,
            from_account INTEGER,
            to_account INTEGER,
            amount REAL NOT NULL,
            timestamp TEXT DEFAULT CURRENT_TIMESTAMP
        )""",
        "CREATE INDEX idx_accounts_name ON accounts(name)"
    ])
    
    if len(setup_results) != 3:
        fail("Expected 3 setup results, got {}".format(len(setup_results)))
    print("Setup completed. Results:", setup_results)
    
    # Insert initial data using batch with parameters
    initial_data = db.batch([
        ["INSERT INTO accounts (name, balance) VALUES (?, ?)", ["Alice", 1000.0]],
        ["INSERT INTO accounts (name, balance) VALUES (?, ?)", ["Bob", 500.0]],
        ["INSERT INTO accounts (name, balance) VALUES (?, ?)", ["Charlie", 750.0]]
    ])
    
    if len(initial_data) != 3:
        fail("Expected 3 initial data results, got {}".format(len(initial_data)))
    print("Initial data inserted. Results:", initial_data)
    
    # Verify initial data
    accounts = db.query("SELECT * FROM accounts ORDER BY name")
    if len(accounts) != 3:
        fail("Expected 3 accounts, got {}".format(len(accounts)))
    
    # Perform a money transfer using batch operations
    transfer_amount = 200.0
    transfer_results = db.batch([
        ["UPDATE accounts SET balance = balance - ? WHERE name = ?", [transfer_amount, "Alice"]],
        ["UPDATE accounts SET balance = balance + ? WHERE name = ?", [transfer_amount, "Bob"]],
        ["INSERT INTO transactions (from_account, to_account, amount) VALUES (?, ?, ?)", [1, 2, transfer_amount]]
    ])
    
    if len(transfer_results) != 3:
        fail("Expected 3 transfer results, got {}".format(len(transfer_results)))
    print("Transfer completed. Results:", transfer_results)
    
    # Test mixed batch (some with params, some without)
    mixed_results = db.batch([
        "UPDATE accounts SET balance = 1000.0 WHERE id = 3",
        ["INSERT INTO accounts (name, balance) VALUES (?, ?)", ["David", 300.0]],
        "DELETE FROM transactions WHERE amount < 50.0"
    ])
    
    if len(mixed_results) != 3:
        fail("Expected 3 mixed results, got {}".format(len(mixed_results)))
    
    # Verify the results
    final_accounts = db.query("SELECT * FROM accounts ORDER BY name")
    expected_names = ["Alice", "Bob", "Charlie", "David"]
    if len(final_accounts) != 4:
        fail("Expected 4 accounts after all operations, got {}".format(len(final_accounts)))
    
    for i, account in enumerate(final_accounts):
        if account["name"] != expected_names[i]:
            fail("Account {} name mismatch: expected {}, got {}".format(i, expected_names[i], account["name"]))
    
    # Verify specific balances
    alice_balance = final_accounts[0]["balance"]  # Alice
    bob_balance = final_accounts[1]["balance"]    # Bob
    
    if alice_balance != 800.0:  # 1000 - 200
        fail("Alice balance should be 800.0, got {}".format(alice_balance))
    if bob_balance != 700.0:    # 500 + 200
        fail("Bob balance should be 700.0, got {}".format(bob_balance))
    
    # Check transaction history
    transactions = db.query("SELECT * FROM transactions")
    if len(transactions) != 1:
        fail("Expected 1 transaction, got {}".format(len(transactions)))
    
    tx = transactions[0]
    if tx["from_account"] != 1 or tx["to_account"] != 2 or tx["amount"] != 200.0:
        fail("Transaction data incorrect: from={}, to={}, amount={}".format(
            tx["from_account"], tx["to_account"], tx["amount"]))
    
    # Close the connection
    db.close()
    
    print("✓ All batch operation tests passed")

main()
`},
		{"AttachDetach", `
load("sqlite", "connect")

def main():
    # Connect to main in-memory database
    main_db = connect(":memory:")
    
    # Create a table in the main database
    main_db.execute("""
        CREATE TABLE current_users (
            id INTEGER PRIMARY KEY,
            name TEXT NOT NULL,
            email TEXT,
            active INTEGER DEFAULT 1
        )
    """)
    
    # Insert some data
    main_db.insert_many("current_users", [
        {"name": "Alice", "email": "alice@example.com"},
        {"name": "Bob", "email": "bob@example.com"},
        {"name": "Charlie", "email": "charlie@example.com"}
    ])
    
    # Verify data was inserted
    rows = main_db.query("SELECT * FROM current_users")
    if len(rows) != 3:
        fail("Expected 3 rows in current_users, found {}".format(len(rows)))
    print("Main database users: {}".format(len(rows)))
    
    # Attach another in-memory database as "archive"
    main_db.attach(":memory:", "archive")
    
    # Create a table in the attached database
    main_db.execute("""
        CREATE TABLE archive.old_users (
            id INTEGER PRIMARY KEY,
            name TEXT NOT NULL,
            email TEXT,
            deactivated_date TEXT DEFAULT CURRENT_TIMESTAMP
        )
    """)
    
    # Insert a user directly into the archive database
    main_db.execute("""
        INSERT INTO archive.old_users (name, email)
        VALUES (?, ?)
    """, ["David", "david@example.com"])
    
    # Query from the attached database
    archived_users = main_db.query("SELECT * FROM archive.old_users")
    if len(archived_users) != 1:
        fail("Expected 1 user in archive.old_users, found {}".format(len(archived_users)))
    
    print("Archived users:")
    for user in archived_users:
        print("  {} ({})".format(user["name"], user["email"]))
        if user["name"] != "David" or user["email"] != "david@example.com":
            fail("Expected David/david@example.com in archive, got {}/{}".format(
                user["name"], user["email"]))
    
    # Detach the archive database
    main_db.detach("archive")
    
    # Verify that the main database is still accessible
    main_users = main_db.query("SELECT * FROM current_users")
    if len(main_users) != 3:
        fail("Expected 3 users in current_users after detach, found {}".format(len(main_users)))
    
    print("✓ All attach/detach tests passed")
    
    # Close the main database connection
    main_db.close()

main()
`},
		{"ErrorHandling", `
load("sqlite", "connect")

def explain_error_scenarios():
    # This function simply explains error scenarios without executing them
    # since Starlark doesn't have try/except blocks
    print("In Starlark, common SQLite errors would halt execution:")
    print("- Creating a table that already exists")
    print("- SQL syntax errors")
    print("- Constraint violations")
    print("- Primary key conflicts")
    print("All would cause execution to stop with a descriptive error message")

def main():
    # Connect to an in-memory database
    db = connect(":memory:")
    
    # Create a table for testing
    db.execute("CREATE TABLE test_error (id INTEGER PRIMARY KEY, name TEXT NOT NULL)")
    
    # Verify table was created
    tables = db.tables()
    print("Tables: {}".format(tables))
    if "test_error" not in tables:
        fail("test_error table should exist")
    
    # Insert a record
    db.execute("INSERT INTO test_error (id, name) VALUES (?, ?)", [1, "test1"])
    
    # Verify the insert
    rows = db.query("SELECT * FROM test_error WHERE id = 1")
    if len(rows) != 1:
        fail("Expected 1 row, got {}".format(len(rows)))
    print("Verified insert: {}".format(rows[0]["name"]))
    
    # Test transaction with explicit commit
    tx = db.begin()
    insert_result = tx.execute("INSERT INTO test_error (id, name) VALUES (?, ?)", [2, "test2"])
    if insert_result.ok:
        commit_result = tx.commit()
        if not commit_result.ok:
            fail("Failed to commit transaction: {}".format(commit_result.error))
    else:
        fail("Failed to insert test record: {}".format(insert_result.error))
    
    # Verify commit worked
    committed = db.query("SELECT * FROM test_error WHERE id = 2")
    if len(committed) != 1:
        fail("Expected committed record to be visible")
    print("Verified commit: {}".format(committed[0]["name"]))
    
    # Test transaction with rollback
    tx2 = db.begin()
    insert2_result = tx2.execute("INSERT INTO test_error (id, name) VALUES (?, ?)", [3, "test3"])
    if not insert2_result.ok:
        fail("Failed to insert for rollback test: {}".format(insert2_result.error))
    
    # Data should be visible within transaction
    query_result = tx2.query("SELECT * FROM test_error WHERE id = 3")
    if not query_result.ok:
        fail("Failed to query within transaction: {}".format(query_result.error))
    
    in_tx = query_result.value
    if len(in_tx) != 1:
        fail("Expected to see record within transaction")
    # But rollback the transaction
    tx2.rollback()
    
    # Verify the rollback worked
    after_rollback = db.query("SELECT * FROM test_error WHERE id = 3")
    if len(after_rollback) > 0:
        fail("Expected no records after rollback")
    print("Verified rollback successful")
    
    # Explain potential error scenarios
    explain_error_scenarios()
    
    # Close the connection
    db.close()
    
    print("✓ All error handling tests passed")

main()
`},
		{"SchemaOperations", `
load("sqlite", "connect")

def main():
    # Connect to an in-memory database
    db = connect(":memory:")
    
    # Create a table with multiple columns for testing
    db.execute("""
        CREATE TABLE products (
            id INTEGER PRIMARY KEY,
            name TEXT NOT NULL,
            category TEXT,
            price REAL,
            stock INTEGER DEFAULT 0
        )
    """)
    
    # Add some indices to the table
    db.execute("CREATE INDEX idx_products_name ON products (name)")
    db.execute("CREATE INDEX idx_products_category ON products (category)")
    db.execute("CREATE UNIQUE INDEX idx_products_combined ON products (name, category)")
    
    # Verify the indices were created
    indices_list = db.indices("products")
    print("Indices for products table:")
    for idx in indices_list:
        print("  - {} (SQL: {})".format(idx["name"], idx["sql"]))
    
    # Verify we have the correct number of indices
    if len(indices_list) != 3:
        fail("Expected 3 indices, found {}".format(len(indices_list)))
    
    # Check for specific indices
    index_names = [idx["name"] for idx in indices_list]
    expected_indices = ["idx_products_name", "idx_products_category", "idx_products_combined"]
    
    for idx_name in expected_indices:
        if idx_name not in index_names:
            fail("Expected index {} not found".format(idx_name))
    
    # Insert some test data
    db.insert_many("products", [
        {"name": "Laptop", "category": "Electronics", "price": 999.99, "stock": 10},
        {"name": "Smartphone", "category": "Electronics", "price": 699.99, "stock": 20},
        {"name": "Headphones", "category": "Accessories", "price": 149.99, "stock": 30},
        {"name": "Keyboard", "category": "Accessories", "price": 89.99, "stock": 15}
    ])
    
    # Verify records were inserted
    count = db.count("products", "")
    if count != 4:
        fail("Expected 4 products, found {}".format(count))
    print("Inserted {} product records".format(count))
    
    # Test truncate_table functionality
    db.truncate_table("products")
    
    # Verify the table is empty but still exists
    count_after_truncate = db.count("products", "")
    if count_after_truncate != 0:
        fail("Table should be empty after truncate, found {} records".format(count_after_truncate))
    print("Table truncated successfully, {} records remaining".format(count_after_truncate))
    
    # Verify the table structure is still intact
    if not db.table_exists("products"):
        fail("Table should still exist after truncate")
    
    # Verify indices are still present after truncate
    indices_after_truncate = db.indices("products")
    if len(indices_after_truncate) != 3:
        fail("Indices should remain after truncate")
    print("Table structure and indices preserved after truncate")
    
    # Insert one record to verify the table is still usable
    db.insert("products", {"name": "Test", "category": "Test", "price": 10.0, "stock": 1})
    count_after_insert = db.count("products", "")
    if count_after_insert != 1:
        fail("Failed to insert after truncate")
    print("Successfully inserted record after truncate")
    
    # Test drop_table functionality
    db.drop_table("products")
    
    # Verify the table no longer exists
    if db.table_exists("products"):
        fail("Table should not exist after drop_table")
    print("Table dropped successfully")
    
    # Verify we can create the table again after dropping it
    db.create_table("products", {
        "id": "INTEGER PRIMARY KEY",
        "name": "TEXT NOT NULL",
        "price": "REAL DEFAULT 0.0"
    })
    
    if not db.table_exists("products"):
        fail("Failed to recreate table after dropping")
    print("Successfully recreated table after dropping")
    
    # Insert a record in the new table to verify it works
    product_id = db.insert("products", {"name": "New Product", "price": 49.99})
    if product_id != 1:
        fail("Expected first product ID to be 1, got {}".format(product_id))
    
    # Close the connection
    db.close()
    
    print("✓ All schema operations tests passed")

main()
`},
		{"ComplexDataTypes", `
load("sqlite", "connect")

def main():
    # Connect to an in-memory database
    db = connect(":memory:")
    
    # Create a table with a column for JSON data
    db.execute("""
        CREATE TABLE complex_data (
            id INTEGER PRIMARY KEY,
            name TEXT NOT NULL,
            metadata TEXT,
            tags TEXT,
            config TEXT
        )
    """)
    
    # Test dictionary storage - metadata
    user_metadata = {
        "last_login": "2023-08-15T14:30:00Z",
        "preferences": {
            "theme": "dark",
            "notifications": True,
            "language": "en-US"
        },
        "usage_stats": {
            "visits": 42,
            "actions": 156,
            "avg_session_time": 340.5
        }
    }
    
    # Test list storage - tags
    user_tags = ["developer", "premium", "beta-tester"]
    
    # Test nested complex structures - config
    user_config = {
        "permissions": ["read", "write", "admin"],
        "rate_limits": {
            "api_calls": {
                "limit": 1000,
                "period": "daily",
                "overage_policy": {
                    "block": False,
                    "throttle": True,
                    "cost_multiplier": 1.5
                }
            },
            "downloads": {
                "limit": 50,
                "period": "hourly"
            }
        },
        "features": [
            {"id": "feature1", "enabled": True},
            {"id": "feature2", "enabled": False},
            {"id": "feature3", "enabled": True, "limits": {"max_usage": 100}}
        ]
    }
    
    # Insert data with complex structures
    user_id = db.insert("complex_data", {
        "name": "John Doe",
        "metadata": user_metadata,
        "tags": user_tags,
        "config": user_config
    })
    
    print("Inserted user with ID: {}".format(user_id))
    
    # Insert another record with slightly different data
    db.insert("complex_data", {
        "name": "Jane Smith",
        "metadata": {
            "last_login": "2023-08-16T09:45:00Z",
            "preferences": {
                "theme": "light",
                "notifications": False,
                "language": "fr-FR"
            },
            "usage_stats": {
                "visits": 28,
                "actions": 93,
                "avg_session_time": 210.75
            }
        },
        "tags": ["designer", "free-tier"],
        "config": {
            "permissions": ["read", "write"],
            "rate_limits": {
                "api_calls": {
                    "limit": 500,
                    "period": "daily"
                }
            },
            "features": [
                {"id": "feature1", "enabled": True},
                {"id": "feature2", "enabled": True}
            ]
        }
    })
    
    # Retrieve the data
    user = db.query_one("SELECT * FROM complex_data WHERE id = ?", [1])
    
    # Test metadata retrieval and conversion
    print("Retrieved user metadata type: {}".format(type(user["metadata"])))
    
    # In SQLite, complex data is stored as JSON strings
    # But when queried back, it's returned as a string in Starlark
    
    # Test JSON search if supported by SQLite version
    print("Testing complex data operations")
    
    # Update complex data with new values
    updated_metadata = {
        "last_login": "2023-08-17T10:00:00Z",
        "preferences": {
            "theme": "dark",
            "notifications": True,
            "language": "en-US"
        },
        "usage_stats": {
            "visits": 43,  # Incremented
            "actions": 160,  # Incremented
            "avg_session_time": 350.2  # Updated
        }
    }
    
    # Append a new tag to the existing tags
    updated_tags = user_tags + ["advanced"]
    
    # Update the record
    db.update("complex_data", 
        {"metadata": updated_metadata, "tags": updated_tags}, 
        ["id = ?", 1]
    )
    
    # Retrieve updated data
    updated_user = db.query_one("SELECT * FROM complex_data WHERE id = ?", [1])
    print("Updated user metadata stored as: {}".format(type(updated_user["metadata"])))
    
    # Demonstrate more complex structures - array of dictionaries
    db.execute("""
        CREATE TABLE products (
            id INTEGER PRIMARY KEY,
            name TEXT,
            attributes TEXT,
            pricing TEXT,
            inventory TEXT
        )
    """)
    
    # Insert product with array of dictionaries
    db.insert("products", {
        "name": "Smartphone XL",
        "attributes": [
            {"name": "color", "value": "black", "filterable": True},
            {"name": "storage", "value": "128GB", "filterable": True},
            {"name": "weight", "value": "155g", "filterable": False},
            {"name": "dimensions", "value": "150x75x8mm", "filterable": False}
        ],
        "pricing": {
            "base": 699.99,
            "discounts": [
                {"type": "sale", "amount": 50.00, "valid_until": "2023-12-31"},
                {"type": "bundle", "amount": 100.00, "requires": ["case", "charger"]}
            ],
            "tax_rate": 0.08,
            "shipping": {
                "standard": 9.99,
                "express": 19.99,
                "free_threshold": 999.00
            }
        },
        "inventory": {
            "total": 120,
            "locations": {
                "warehouse_a": 45,
                "warehouse_b": 30,
                "store_1": 25,
                "store_2": 20
            },
            "restock_threshold": 25,
            "on_order": True
        }
    })
    
    # Query product
    product = db.query_one("SELECT * FROM products WHERE id = 1")
    if product:
        print("Successfully stored and retrieved complex product data")
    
    # Close the connection
    db.close()
    
    print("✓ All complex data type tests passed")

main()
`},
		{"ComplexDataTypeEdgeCases", `
load("sqlite", "connect")

def main():
    # Connect to an in-memory database
    db = connect(":memory:")
    
    # Create a table specifically for testing complex data types
    db.execute("""
        CREATE TABLE complex_types (
            id INTEGER PRIMARY KEY,
            test_name TEXT NOT NULL,
            dict_data TEXT,
            list_data TEXT,
            mixed_data TEXT
        )
    """)
    
    # 1. Test empty dict and list
    empty_dict = {}
    empty_list = []
    
    id1 = db.insert("complex_types", {
        "test_name": "empty_structures",
        "dict_data": empty_dict,
        "list_data": empty_list,
        "mixed_data": {"empty_list": empty_list}
    })
    
    # 2. Test Dict with all primitive data types
    all_primitives = {
        "null_value": None,
        "bool_true": True,
        "bool_false": False,
        "int_value": 12345,
        "float_value": 123.456,
        "string_value": "Hello, world!",
        "special_chars": "!@#$%^&*()\n\t\"'\\/"
    }
    
    id2 = db.insert("complex_types", {
        "test_name": "all_primitives",
        "dict_data": all_primitives,
        "list_data": [],
        "mixed_data": None
    })
    
    # 3. Test List with mixed types
    mixed_list = [
        None,
        True,
        False,
        42,
        3.14159,
        "string",
        ["nested", "list"],
        {"nested": "dict"}
    ]
    
    id3 = db.insert("complex_types", {
        "test_name": "mixed_list",
        "dict_data": {},
        "list_data": mixed_list,
        "mixed_data": None
    })
    
    # 4. Test deeply nested structures
    deep_nesting = {
        "level1": {
            "level2": {
                "level3": {
                    "level4": {
                        "level5": {
                            "value": "deeply nested",
                            "list": [1, [2, [3, [4, [5]]]]]
                        }
                    }
                }
            }
        }
    }
    
    id4 = db.insert("complex_types", {
        "test_name": "deep_nesting",
        "dict_data": deep_nesting,
        "list_data": [],
        "mixed_data": None
    })
    
    # 5. Test Dict with numeric keys (will be converted to strings in JSON)
    numeric_keys = {
        "0": "zero",
        "1": "one",
        "2": "two"
    }
    
    id5 = db.insert("complex_types", {
        "test_name": "numeric_keys",
        "dict_data": numeric_keys,
        "list_data": [],
        "mixed_data": None
    })
    
    # 6. Test extremely large list
    large_list = list(range(1000))
    
    id6 = db.insert("complex_types", {
        "test_name": "large_list",
        "dict_data": {},
        "list_data": large_list,
        "mixed_data": None
    })
    
    # 7. Test dict with unicode characters
    unicode_dict = {
        "emoji": "😀🙂🤔👍",
        "chinese": "你好，世界",
        "arabic": "مرحبا بالعالم",
        "russian": "Привет, мир",
        "greek": "Γειά σου Κόσμε"
    }
    
    id7 = db.insert("complex_types", {
        "test_name": "unicode_dict",
        "dict_data": unicode_dict,
        "list_data": [],
        "mixed_data": None
    })
    
    # Verify data was properly saved by retrieving and checking values
    print("Testing retrieval and verification of stored complex types...")
    
    # Verify empty structures
    row1 = db.query_one("SELECT * FROM complex_types WHERE id = ?", [id1])
    if not row1:
        fail("Failed to retrieve empty structures row")
    
    # Check round-trip persistence using raw string data
    row2 = db.query_one("SELECT * FROM complex_types WHERE id = ?", [id2])
    print("All primitives stored as type: {}".format(type(row2["dict_data"])))
    if "null_value" not in row2["dict_data"] or "int_value" not in row2["dict_data"]:
        fail("Failed to store/retrieve primitive values in dict")
    
    # Verify mixed list
    row3 = db.query_one("SELECT * FROM complex_types WHERE id = ?", [id3])
    if not "nested" in row3["list_data"]:
        fail("Failed to store/retrieve nested values in list")
    
    # Verify deep nesting
    row4 = db.query_one("SELECT * FROM complex_types WHERE id = ?", [id4])
    if not "level1" in row4["dict_data"]:
        fail("Failed to store/retrieve deeply nested structure")
    
    # Verify large list
    row6 = db.query_one("SELECT * FROM complex_types WHERE id = ?", [id6])
    if "list_data" not in row6 or len(row6["list_data"]) < 100:
        fail("Failed to store/retrieve large list properly")
    
    # Verify unicode
    row7 = db.query_one("SELECT * FROM complex_types WHERE id = ?", [id7])
    if "emoji" not in row7["dict_data"] or "chinese" not in row7["dict_data"]:
        fail("Failed to store/retrieve unicode data")
    
    # Demonstrate in-transaction complex data updates
    tx = db.begin()
    
    # Update a complex structure within a transaction
    # Create a new dict with the same content plus new item (Starlark has no dict.copy())
    updated_dict = {
        "null_value": None,
        "bool_true": True,
        "bool_false": False,
        "int_value": 12345,
        "float_value": 123.456,
        "string_value": "Hello, world!",
        "special_chars": "!@#$%^&*()\n\t\"\'\\/",
        "new_key": "added in transaction"
    }
    
    update_result = tx.execute(
        "UPDATE complex_types SET dict_data = ? WHERE id = ?",
        [updated_dict, id2]
    )
    if not update_result.ok:
        fail("Failed to update complex data: {}".format(update_result.error))
    
    # Check that data is visible within the transaction
    tx_query_result = tx.query_one("SELECT dict_data FROM complex_types WHERE id = ?", [id2])
    if not tx_query_result.ok:
        fail("Failed to query within transaction: {}".format(tx_query_result.error))
    
    tx_row = tx_query_result.value
    if "new_key" not in tx_row["dict_data"]:
        fail("Transaction update of complex data failed")
    
    # Commit the transaction
    commit_result = tx.commit()
    if not commit_result.ok:
        fail("Failed to commit complex data transaction: {}".format(commit_result.error))
    
    # Verify update persisted
    after_tx = db.query_one("SELECT dict_data FROM complex_types WHERE id = ?", [id2])
    if "new_key" not in after_tx["dict_data"]:
        fail("Transaction commit didn't persist complex data update")
    
    print("Successfully verified complex type handling")
    
    # Close the connection
    db.close()
    
    print("✓ All complex data type edge cases passed")

main()
`},
		{"BinaryData", `
load("sqlite", "connect")

def main():
    # Connect to an in-memory database
    db = connect(":memory:")
    
    # Create a table with a BLOB column
    db.execute("""
        CREATE TABLE binary_data (
            id INTEGER PRIMARY KEY,
            name TEXT NOT NULL,
            data BLOB
        )
    """)
    
    # Create some binary data using bytes type
    # In Starlark, binary data is represented using the 'bytes' type
    binary_data1 = bytes([0, 1, 2, 3, 4, 5, 255, 254, 253, 252])
    binary_data2 = bytes([10, 20, 30, 40, 50, 60, 70, 80, 90])
    
    # Test data with null bytes and special characters
    binary_data3 = bytes([0, 0, 0, 65, 66, 67, 0, 0, 0])  # Null bytes with "ABC" in the middle
    
    # Insert binary data
    id1 = db.insert("binary_data", {
        "name": "sample1",
        "data": binary_data1
    })
    
    id2 = db.insert("binary_data", {
        "name": "sample2",
        "data": binary_data2
    })
    
    id3 = db.insert("binary_data", {
        "name": "sample3",
        "data": binary_data3
    })
    
    # Retrieve and verify binary data
    row1 = db.query_one("SELECT * FROM binary_data WHERE id = ?", [id1])
    row2 = db.query_one("SELECT * FROM binary_data WHERE id = ?", [id2])
    row3 = db.query_one("SELECT * FROM binary_data WHERE id = ?", [id3])
    
    # Verify retrieved data is still in bytes type
    print("Retrieved binary data type: {}".format(type(row1["data"])))
    
    # Verify binary data content
    if row1["data"] != binary_data1:
        fail("Binary data 1 didn't match after round-trip")
    
    if row2["data"] != binary_data2:
        fail("Binary data 2 didn't match after round-trip")
    
    if row3["data"] != binary_data3:
        fail("Binary data 3 didn't match after round-trip")
    
    # Update binary data
    updated_data = bytes([255, 255, 255, 0, 0, 0])
    db.update("binary_data", {"data": updated_data}, ["id = ?", id1])
    
    # Verify update
    updated_row = db.query_one("SELECT * FROM binary_data WHERE id = ?", [id1])
    if updated_row["data"] != updated_data:
        fail("Updated binary data didn't match after round-trip")
    
    # Test with a prepared statement
    stmt = db.prepare("INSERT INTO binary_data (name, data) VALUES (?, ?)")
    sample4_data = bytes([1, 3, 5, 7, 9, 11, 13])
    stmt.execute(["sample4", sample4_data])
    stmt.close()
    
    # Verify prepared statement insert
    row4 = db.query_one("SELECT * FROM binary_data WHERE name = ?", ["sample4"])
    if row4["data"] != sample4_data:
        fail("Binary data 4 didn't match after prepared statement insert")
    
    print("All binary data tests passed successfully")
    
    # Close the connection
    db.close()
    
    print("✓ All binary data tests passed")

main()
`},
		{"EnhancedCreateTable", `
load("sqlite", "connect")

def main():
    """Test enhanced create_table functionality with structured columns, constraints, and indexes."""
    print("Testing enhanced create_table functionality...")

    db = connect(":memory:")

    # Test 1: Backward compatibility - simple string columns
    print("Test 1: Backward compatibility")
    db.create_table("simple_users", {
        "id": "INTEGER PRIMARY KEY",
        "name": "TEXT NOT NULL",
        "email": "TEXT UNIQUE"
    })
    
    # Verify table was created
    if not db.table_exists("simple_users"):
        fail("simple_users table should exist")
    
    # Insert and verify basic functionality
    db.insert("simple_users", {"name": "Alice", "email": "alice@test.com"})
    users = db.query("SELECT * FROM simple_users")
    if len(users) != 1 or users[0]["name"] != "Alice":
        fail("Basic functionality should work with simple columns")
    print("✓ Backward compatibility works")

    # Test 2: Structured column definitions
    print("Test 2: Structured column definitions")
    db.create_table("structured_users", {
        "id": {
            "type": "INTEGER",
            "primary_key": True,
            "autoincrement": True
        },
        "username": {
            "type": "TEXT",
            "not_null": True,
            "unique": True
        },
        "email": {
            "type": "TEXT",
            "not_null": True
        },
        "age": {
            "type": "INTEGER",
            "default": 0
        },
        "is_active": {
            "type": "BOOLEAN",
            "default": True
        },
        "bio": {
            "type": "TEXT"
        }
    })
    
    # Test the structured table
    user_id = db.insert("structured_users", {
        "username": "bob",
        "email": "bob@test.com",
        "age": 25
    })
    
    if user_id <= 0:
        fail("Should get a valid user ID from autoincrement")
    
    # Verify default values work
    user = db.query_one("SELECT * FROM structured_users WHERE id = ?", [user_id])
    if user["is_active"] != 1:  # SQLite stores booleans as integers
        fail("Default boolean value should be 1 (True)")
    if user["age"] != 25:
        fail("Age should be 25")
    print("✓ Structured column definitions work")

    # Test 3: Table constraints
    print("Test 3: Table constraints")
    db.create_table("posts", {
        "id": "INTEGER PRIMARY KEY",
        "user_id": "INTEGER NOT NULL",
        "title": "TEXT NOT NULL",
        "content": "TEXT",
        "category": "TEXT",
        "status": "TEXT DEFAULT 'draft'"
    }, constraints=[
        "FOREIGN KEY (user_id) REFERENCES structured_users(id) ON DELETE CASCADE",
        "CHECK (length(title) > 0)",
        "UNIQUE (user_id, title)"
    ])
    
    # Test constraint functionality
    post_id = db.insert("posts", {
        "user_id": user_id,
        "title": "Test Post",
        "content": "This is a test post"
    })
    
    if post_id <= 0:
        fail("Should get a valid post ID")
    
    # Verify the post was inserted
    post = db.query_one("SELECT * FROM posts WHERE id = ?", [post_id])
    if post["title"] != "Test Post":
        fail("Post should be inserted correctly")
    print("✓ Table constraints work")

    # Test 4: Simple indexes
    print("Test 4: Simple indexes")
    db.create_table("products", {
        "id": "INTEGER PRIMARY KEY",
        "name": "TEXT NOT NULL",
        "category": "TEXT",
        "price": "REAL",
        "created_at": "TEXT DEFAULT CURRENT_TIMESTAMP"
    }, indexes=[
        "name",                    # Single column index
        "category",                # Another single column index  
        ["category", "price"],     # Composite index
        ["created_at"]             # Single column in list (should work)
    ])
    
    # Insert some test data
    db.insert("products", {"name": "Laptop", "category": "Electronics", "price": 999.99})
    db.insert("products", {"name": "Mouse", "category": "Electronics", "price": 29.99})
    db.insert("products", {"name": "Desk", "category": "Furniture", "price": 199.99})
    
    # Verify data was inserted
    products = db.query("SELECT * FROM products ORDER BY price")
    if len(products) != 3:
        fail("Should have 3 products")
    if products[0]["name"] != "Mouse":  # Cheapest should be first
        fail("Products should be ordered by price")
    
    # Test that indices were created by checking sqlite_master
    indices = db.query("SELECT name FROM sqlite_master WHERE type='index' AND tbl_name='products'")
    index_names = [idx["name"] for idx in indices]
    
    # Check for our created indices (exclude automatic ones)
    expected_indices = ["idx_products_name", "idx_products_category", "idx_products_category_price", "idx_products_created_at"]
    for expected in expected_indices:
        if expected not in index_names:
            print("Available indices: {}".format(index_names))
            fail("Expected index {} was not created".format(expected))
    
    print("✓ Simple indexes work")

    # Test 5: Mixed column definitions (string + structured)
    print("Test 5: Mixed column definitions")
    db.create_table("mixed_table", {
        "id": "INTEGER PRIMARY KEY AUTOINCREMENT",  # String definition
        "name": {                                   # Structured definition
            "type": "TEXT",
            "not_null": True
        },
        "email": "TEXT UNIQUE",                     # String definition
        "created_at": {                             # Structured definition
            "type": "TEXT",
            "default": "CURRENT_TIMESTAMP"
        }
    })
    
    # Test mixed table
    mixed_id = db.insert("mixed_table", {"name": "Charlie", "email": "charlie@test.com"})
    if mixed_id <= 0:
        fail("Should get a valid ID from mixed table")
    
    mixed_row = db.query_one("SELECT * FROM mixed_table WHERE id = ?", [mixed_id])
    if mixed_row["name"] != "Charlie":
        fail("Mixed table should work correctly")
    print("✓ Mixed column definitions work")

    # Test 6: Error handling
    print("Test 6: Error handling")
    
    # Test table that already exists (this should fail)
    # Note: In Starlark we can't use try/catch, so we just verify the success cases
    # Error cases would cause the script to fail which is the expected behavior
    
    # Test invalid column type in structured definition
    # This would be caught in real usage, but we test valid usage here
    
    print("✓ Error handling works as expected (errors cause script termination)")

    # Test 7: Table info verification
    print("Test 7: Verify table structure")
    
    # Check the structured_users table info
    table_info = db.table_info("structured_users")
    
    # Find the username column and verify it has NOT NULL
    username_col = None
    for col in table_info:
        if col["name"] == "username":
            username_col = col
            break
    
    if not username_col:
        fail("username column should exist")
    
    if username_col["notnull"] != 1:
        fail("username column should be NOT NULL")
    
    if username_col["type"] != "TEXT":
        fail("username column should be TEXT type")
    
    print("✓ Table structure verification works")

    # Close the connection
    db.close()
    
    print("✓ All enhanced create_table tests passed!")

main()
`},
		{"CustomSQLFunctions", `
load("sqlite", "connect", "register_function")

def main():
    # Test 1: Register simple string function
    print("Test 1: Simple string function")
    register_function("EXAMPLE_TRIM", lambda s: s.strip() if s else "")
    
    # Test 2: Register mathematical function  
    print("Test 2: Mathematical function")
    register_function("EXAMPLE_SQUARE", lambda x: x * x if x else 0, num_args=1, deterministic=True)
    
    # Test 3: Register string manipulation function
    print("Test 3: String manipulation function")
    register_function("EXAMPLE_REVERSE", lambda s: s[::-1] if s else "", num_args=1)
    
    # Test 4: Register variadic function (min of multiple values)
    print("Test 4: Variadic function")
    def min_values(*args):
        """Return minimum value from multiple arguments"""
        if not args:
            return None
        non_null_args = []
        for arg in args:
            if arg != None:
                non_null_args.append(arg)
        if not non_null_args:
            return None
        return min(non_null_args)
    
    register_function("EXAMPLE_MIN_OF", min_values, num_args=-1, deterministic=True)
    
    # Test 5: Register function that returns complex data as JSON
    print("Test 5: Complex data function")
    def get_stats(*args):
        """Return statistics as JSON string"""
        sum = 0
        if not args:
            return "{}"
        non_null = []
        for arg in args:
            if arg != None:
                non_null.append(arg)
                sum += arg
        if not non_null:
            return "{}"
        
        stats = {
            "count": len(non_null),
            "sum": sum,
            "avg": sum / len(non_null),
            "min": min(non_null),
            "max": max(non_null)
        }
        return stats  # Will be JSON-encoded automatically
    
    register_function("EXAMPLE_STATS", get_stats, num_args=-1)
    
    # Now test the functions with actual SQL queries
    db = connect(":memory:")
    
    # Create test table
    db.execute("CREATE TABLE test_data (id INTEGER, name TEXT, value REAL)")
    db.execute("INSERT INTO test_data VALUES (1, '  Alice  ', 10.5)")
    db.execute("INSERT INTO test_data VALUES (2, '  Bob  ', 20.3)")
    db.execute("INSERT INTO test_data VALUES (3, '  Charlie  ', 15.7)")
    
    print("Created test data")
    
    # Test MY_TRIM function
    result = db.query("SELECT id, EXAMPLE_TRIM(name) as trimmed_name FROM test_data ORDER BY id")
    print("Trimmed names:")
    for row in result:
        print("  ID {}: '{}'".format(row["id"], row["trimmed_name"]))
    
    # Test SQUARE function  
    result = db.query("SELECT id, name, value, EXAMPLE_SQUARE(value) as squared FROM test_data ORDER BY id")
    print("Squared values:")
    for row in result:
        print("  {}: {} -> {}".format(row["name"].strip(), row["value"], row["squared"]))
    
    # Test REVERSE_STR function
    result = db.query("SELECT id, name, EXAMPLE_REVERSE(EXAMPLE_TRIM(name)) as reversed FROM test_data ORDER BY id")
    print("Reversed names:")
    for row in result:
        print("  {}: '{}'".format(row["name"].strip(), row["reversed"]))
    
    # Test MIN_OF variadic function
    result = db.query("SELECT EXAMPLE_MIN_OF(10.5, 20.3, 15.7, 5.2) as min_val")
    min_val = result[0]["min_val"]
    print("EXAMPLE_MIN_OF(10.5, 20.3, 15.7, 5.2) = {}".format(min_val))
    
    # Test with nulls
    result = db.query("SELECT EXAMPLE_MIN_OF(10.5, NULL, 15.7) as min_val")
    min_val = result[0]["min_val"]
    print("EXAMPLE_MIN_OF(10.5, NULL, 15.7) = {}".format(min_val))
    
    # Test GET_STATS function
    result = db.query("SELECT EXAMPLE_STATS(10.5, 20.3, 15.7) as stats")
    stats = result[0]["stats"]
    print("EXAMPLE_STATS(10.5, 20.3, 15.7) = {}".format(stats))
    
    # Test function in WHERE clause (deterministic functions)
    result = db.query("SELECT id, name, value FROM test_data WHERE EXAMPLE_SQUARE(value) > 300 ORDER BY id")
    print("Records where EXAMPLE_SQUARE(value) > 300:")
    for row in result:
        print("  {}: value={}, square={}".format(row["name"].strip(), row["value"], row["value"] * row["value"]))
    
    # Test function in ORDER BY
    result = db.query("SELECT id, name, value FROM test_data ORDER BY EXAMPLE_SQUARE(value) DESC")
    print("Records ordered by EXAMPLE_SQUARE(value) DESC:")
    for row in result:
        print("  {}: value={}, square={}".format(row["name"].strip(), row["value"], row["value"] * row["value"]))
    
    db.close()
    
    print("✓ All custom SQL function tests passed")

main()
`},
		{"ExistOkFeature", `
load("sqlite", "connect")

def main():
    """Test the exist_ok parameter for create_table."""
    print("Testing exist_ok parameter functionality...")

    db = connect(":memory:")

    # Test 1: Normal table creation (should succeed)
    print("Test 1: Normal table creation")
    db.create_table("users", {
        "id": "INTEGER PRIMARY KEY",
        "name": "TEXT NOT NULL",
        "email": "TEXT UNIQUE"
    })
    
    # Verify table was created
    if not db.table_exists("users"):
        fail("users table should exist after creation")
    print("✓ Table created successfully")

    # Test 2: Try to create same table again with exist_ok=False (would fail, but we skip this test)
    print("Test 2: Duplicate creation with exist_ok=False would fail (skipped)")

    # Test 3: Create same table again with exist_ok=True (should succeed silently)
    print("Test 3: Duplicate creation with exist_ok=True")
    db.create_table("users", {
        "id": "INTEGER PRIMARY KEY",
        "name": "TEXT NOT NULL",
        "email": "TEXT UNIQUE"
    }, exist_ok=True)
    
    # Table should still exist and be functional
    if not db.table_exists("users"):
        fail("users table should still exist after duplicate creation with exist_ok=True")
    print("✓ Duplicate creation with exist_ok=True succeeded")

    # Test 4: Insert data to verify table is still functional
    print("Test 4: Verify table functionality")
    user_id = db.insert("users", {"name": "Alice", "email": "alice@example.com"})
    if user_id <= 0:
        fail("Should be able to insert into table after exist_ok creation")
    
    users = db.query("SELECT * FROM users WHERE name = ?", ["Alice"])
    if len(users) != 1:
        fail("Should find exactly one user")
    print("✓ Table is functional after exist_ok creation")

    # Test 5: Test with constraints and indexes
    print("Test 5: Complex table with exist_ok=True")
    db.create_table("products", {
        "id": "INTEGER PRIMARY KEY",
        "name": "TEXT NOT NULL",
        "category": "TEXT",
        "price": "REAL DEFAULT 0.0"
    }, constraints=[
        "CHECK (price >= 0)"
    ], indexes=[
        "category",
        ["name", "category"]
    ], exist_ok=True)
    
    # Try to create the same table again with different schema (should still succeed silently)
    db.create_table("products", {
        "id": "INTEGER PRIMARY KEY",
        "different_column": "TEXT"  # Different schema
    }, exist_ok=True)
    
    # Verify original table structure is preserved
    if not db.table_exists("products"):
        fail("products table should exist")
    
    # Insert test data to verify original schema is preserved
    product_id = db.insert("products", {"name": "Laptop", "category": "Electronics", "price": 999.99})
    if product_id <= 0:
        fail("Should be able to insert with original schema")
    print("✓ Complex table with exist_ok=True works correctly")

    # Test 6: Idempotent setup pattern
    print("Test 6: Idempotent setup pattern")
    
    # This pattern is useful for migrations and setup scripts
    tables_to_create = [
        ("logs", {
            "id": "INTEGER PRIMARY KEY",
            "message": "TEXT NOT NULL",
            "timestamp": "TEXT DEFAULT CURRENT_TIMESTAMP"
        }),
        ("config", {
            "key": "TEXT PRIMARY KEY",
            "value": "TEXT"
        }),
        ("cache", {
            "key": "TEXT PRIMARY KEY",
            "value": "TEXT",
            "expires_at": "TEXT"
        })
    ]
    
    # Create all tables (safe to run multiple times)
    for table_name, schema in tables_to_create:
        db.create_table(table_name, schema, exist_ok=True)
    
    # Verify all tables exist
    all_tables = db.tables()
    expected_tables = ["users", "products", "logs", "config", "cache"]
    for expected_table in expected_tables:
        if expected_table not in all_tables:
            fail("Expected table '{}' not found".format(expected_table))
    
    print("✓ Idempotent setup pattern works correctly")

    # Test 7: Mixed parameters with exist_ok
    print("Test 7: All parameters with exist_ok")
    db.create_table("advanced_table", {
        "id": {
            "type": "INTEGER",
            "primary_key": True,
            "autoincrement": True
        },
        "name": {
            "type": "TEXT",
            "not_null": True,
            "unique": True
        },
        "data": {
            "type": "TEXT",
            "default": "{}"
        }
    }, constraints=[
        "CHECK (length(name) > 0)"
    ], indexes=[
        "name"
    ], exist_ok=True)
    
    # Try to create again - should work
    db.create_table("advanced_table", {
        "different_id": "INTEGER PRIMARY KEY"
    }, exist_ok=True)
    
    # Verify original structure preserved
    if not db.table_exists("advanced_table"):
        fail("advanced_table should exist")
    
    # Test that original structure works
    advanced_id = db.insert("advanced_table", {"name": "test_item", "data": '{"key": "value"}'})
    if advanced_id <= 0:
        fail("Should be able to insert with original advanced schema")
    
    print("✓ All parameters with exist_ok work correctly")

    db.close()
    
    print("✓ All exist_ok parameter tests passed!")

main()
`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			base.RunTestScript(t, test.script, "sqlite", func() starlet.ModuleLoader {
				return NewModule().LoadModule()
			}, nil)
		})
	}
}

// TestConnectRemote exercises connect_remote against a libSQL server. It is an
// integration test: set SQLITE_REMOTE_URL (e.g. "http://localhost:8080" for a
// local sqld, or a Turso Cloud URL) and optionally SQLITE_REMOTE_AUTH_TOKEN.
// Without SQLITE_REMOTE_URL it skips, so CI and offline runs are unaffected.
func TestConnectRemote(t *testing.T) {
	remoteURL := os.Getenv("SQLITE_REMOTE_URL")
	if remoteURL == "" {
		t.Skip("set SQLITE_REMOTE_URL (e.g. http://localhost:8080 for a local sqld) to run the remote integration test")
	}
	authToken := os.Getenv("SQLITE_REMOTE_AUTH_TOKEN")
	script := fmt.Sprintf(`
load("sqlite", "connect_remote")

def main():
    db = connect_remote(%q, auth_token=%q)
    db.execute("DROP TABLE IF EXISTS remote_smoke")
    db.execute("CREATE TABLE remote_smoke (id INTEGER PRIMARY KEY, name TEXT)")
    db.execute("INSERT INTO remote_smoke (id, name) VALUES (?, ?)", [1, "star"])
    rows = db.query("SELECT id, name FROM remote_smoke ORDER BY id")
    if len(rows) != 1:
        fail("expected 1 row, got {}".format(len(rows)))
    if rows[0]["name"] != "star":
        fail("unexpected name: {}".format(rows[0]["name"]))
    db.close()

main()
`, remoteURL, authToken)
	requireSQLiteScript(t, script, func() starlet.ModuleLoader {
		return NewModule().LoadModule()
	})
}
