# SQLite Transaction Error Handling Enhancement - Design Document

## Overview

This document outlines the design for enhancing transaction error handling in the SQLite Starlark module. The enhancement allows transaction operations to return structured error results instead of causing script failure, enabling better error handling in Starlark scripts.

## Motivation

**Current Problem:**
- Transaction operations use `fail()` on errors, causing immediate script termination
- No way to handle database errors gracefully in Starlark (no try/catch)
- Users cannot implement custom error handling or recovery logic
- Transaction cleanup must be manual and is often forgotten

**Goals:**
- Enable graceful error handling in transaction operations
- Maintain backward compatibility where possible
- Follow existing code patterns and minimal changes
- Provide structured error information for better debugging

## Technical Design

### 1. Operation Result Type

Create a new Starlark value type to represent operation results:

```go
// OperationResult represents the result of a database operation
type OperationResult struct {
    value starlark.Value  // The actual result value (or starlark.None if error)
    err   error          // The error (or nil if success)
}

// Starlark interface methods
func (r *OperationResult) String() string
func (r *OperationResult) Type() string
func (r *OperationResult) Freeze()
func (r *OperationResult) Truth() starlark.Bool
func (r *OperationResult) Hash() (uint32, error)
func (r *OperationResult) Attr(name string) (starlark.Value, error)
func (r *OperationResult) AttrNames() []string
```

**Starlark Properties:**
- `result.ok` - Boolean indicating success (true if no error)
- `result.error` - String containing error message (empty string if no error)
- `result.value` - The operation result value (None if error occurred)

### 2. API Changes

**Modified Methods in `transaction.go`:**

| Method | Current Behavior | New Behavior |
|--------|------------------|--------------|
| `tx.execute()` | Returns `int64` or fails | Returns `OperationResult` |
| `tx.query()` | Returns `*starlark.List` or fails | Returns `OperationResult` |
| `tx.query_one()` | Returns `starlark.Value` or fails | Returns `OperationResult` |
| `tx.commit()` | Returns `None` or fails | Returns `OperationResult` |
| `tx.rollback()` | Returns `None` or fails | **Unchanged** (still fails) |

**Rationale for keeping `tx.rollback()` unchanged:**
- Rollback failures are rare and usually indicate database corruption
- When rollback fails, the connection is typically unusable
- Script termination is often the appropriate response to rollback failure

### 3. Implementation Plan

**File Changes:**
- **`transaction.go`** - Primary changes (~80 lines total)
  - Add `OperationResult` struct and Starlark wrapper (~40 lines)
  - Modify 4 existing methods to return results (~40 lines)
- **`README.md`** - Documentation updates with new examples

**Implementation Steps:**
1. Add `OperationResult` type with Starlark interface
2. Create helper functions for success/error result creation
3. Modify transaction methods to wrap results instead of returning errors
4. Update unit tests to use new result-based API
5. Add example scripts demonstrating error handling patterns

## Usage Examples

### Basic Error Handling Pattern

```python
load("sqlite", "connect")

def transfer_money(db, from_id, to_id, amount):
    """Transfer money between accounts with error handling."""
    tx = db.begin()
    
    # Check sender balance
    balance_result = tx.query_one("SELECT balance FROM accounts WHERE id = ?", [from_id])
    if not balance_result.ok:
        print("Failed to check balance: {}".format(balance_result.error))
        tx.rollback()
        return False
    
    if balance_result.value["balance"] < amount:
        print("Insufficient funds")
        tx.rollback()
        return False
    
    # Perform debit
    debit_result = tx.execute("UPDATE accounts SET balance = balance - ? WHERE id = ?", [amount, from_id])
    if not debit_result.ok:
        print("Failed to debit account: {}".format(debit_result.error))
        tx.rollback()
        return False
    
    # Perform credit
    credit_result = tx.execute("UPDATE accounts SET balance = balance + ? WHERE id = ?", [amount, to_id])
    if not credit_result.ok:
        print("Failed to credit account: {}".format(credit_result.error))
        tx.rollback()
        return False
    
    # Commit transaction
    commit_result = tx.commit()
    if not commit_result.ok:
        print("Failed to commit transaction: {}".format(commit_result.error))
        return False
    
    print("Transfer successful")
    return True
```

### Batch Operations with Error Recovery

```python
def batch_insert_with_recovery(db, records):
    """Insert multiple records, handling constraint violations gracefully."""
    tx = db.begin()
    successful_inserts = 0
    failed_records = []
    
    for record in records:
        result = tx.execute(
            "INSERT INTO users (name, email) VALUES (?, ?)",
            [record["name"], record["email"]]
        )
        
        if result.ok:
            successful_inserts += 1
            print("Inserted: {}".format(record["name"]))
        else:
            failed_records.append({
                "record": record,
                "error": result.error
            })
            print("Failed to insert {}: {}".format(record["name"], result.error))
    
    if successful_inserts > 0:
        commit_result = tx.commit()
        if commit_result.ok:
            print("Committed {} successful inserts".format(successful_inserts))
        else:
            print("Failed to commit: {}".format(commit_result.error))
            return {"success": False, "error": commit_result.error}
    else:
        tx.rollback()
        print("No successful inserts, transaction rolled back")
    
    return {
        "success": True,
        "inserted": successful_inserts,
        "failed": failed_records
    }
```

## README Documentation Updates

### New Section: "Advanced Transaction Error Handling"

```markdown
### Advanced Transaction Error Handling

Transaction operations return result objects that allow you to handle errors gracefully without script termination:

```python
load("sqlite", "connect")

def main():
    db = connect(":memory:")
    
    # Setup
    db.execute("CREATE TABLE accounts (id INTEGER, balance REAL)")
    db.execute("INSERT INTO accounts VALUES (1, 100.0)")
    
    # Transaction with error handling
    tx = db.begin()
    
    # Operations return result objects
    result = tx.execute("UPDATE accounts SET balance = balance - ? WHERE id = ?", [50.0, 1])
    
    if result.ok:
        print("Debit successful, {} rows affected".format(result.value))
        
        # Commit and check for errors
        commit_result = tx.commit()
        if commit_result.ok:
            print("Transaction committed successfully")
        else:
            print("Commit failed: {}".format(commit_result.error))
    else:
        print("Operation failed: {}".format(result.error))
        tx.rollback()  # Note: rollback() still fails on error (rare case)

main()
```

**Result Object Properties:**
- `result.ok` - Boolean indicating success
- `result.error` - Error message string (empty if no error)
- `result.value` - Operation result (None if error)
```

### Updated Transaction Examples Section

```markdown
### Transaction Examples

#### Basic Transaction
```python
tx = db.begin()
result = tx.execute("UPDATE users SET active = 1 WHERE id = ?", [user_id])
if result.ok:
    tx.commit()
else:
    print("Update failed: {}".format(result.error))
    tx.rollback()
```

#### Transaction with Multiple Operations
```python
def transfer_funds(db, from_account, to_account, amount):
    tx = db.begin()
    
    # Debit source account
    debit = tx.execute(
        "UPDATE accounts SET balance = balance - ? WHERE id = ? AND balance >= ?",
        [amount, from_account, amount]
    )
    
    if not debit.ok or debit.value == 0:  # No rows affected = insufficient funds
        tx.rollback()
        return False
    
    # Credit destination account
    credit = tx.execute(
        "UPDATE accounts SET balance = balance + ? WHERE id = ?",
        [amount, to_account]
    )
    
    if not credit.ok:
        print("Credit failed: {}".format(credit.error))
        tx.rollback()
        return False
    
    # Commit transaction
    commit_result = tx.commit()
    return commit_result.ok
```
```

## Breaking Changes

**Impact:**
- **Transaction methods now return `OperationResult` instead of direct values**
- Scripts using transactions will need updates to handle result objects
- Database operations outside transactions remain unchanged

**Migration Pattern:**
```python
# Before
tx = db.begin()
rows_affected = tx.execute("UPDATE ...")
tx.commit()

# After  
tx = db.begin()
result = tx.execute("UPDATE ...")
if result.ok:
    rows_affected = result.value
    commit_result = tx.commit()
    if not commit_result.ok:
        print("Commit failed: {}".format(commit_result.error))
else:
    print("Execute failed: {}".format(result.error))
    tx.rollback()
```

## Testing Strategy

1. **Unit Tests:** Update existing transaction tests to use new result API
2. **Integration Tests:** Add comprehensive error handling test scenarios
3. **Example Scripts:** Verify all README examples work correctly
4. **Backward Compatibility:** Ensure breaking changes are intentional and documented

## Implementation Timeline

1. **Phase 1:** Implement `OperationResult` type and core functionality
2. **Phase 2:** Update transaction methods to return results
3. **Phase 3:** Update tests and examples
4. **Phase 4:** Update documentation and README

This design provides a clean, minimal enhancement that enables robust error handling while maintaining the existing transaction patterns and requiring only targeted changes to the codebase. 