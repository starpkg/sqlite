# Migration Guide: Transaction Error Handling

This guide shows how to migrate from the old transaction API to the new `OperationResult`-based API that provides better error handling capabilities.

## Overview

The transaction methods (`execute`, `query`, `query_one`, `commit`) now return `OperationResult` objects instead of direct values or causing script failures. This enables graceful error handling without script termination.

## Migration Examples

### Basic Execute Operation

**Before (Old API):**
```python
tx = db.begin()
rows_affected = tx.execute("UPDATE accounts SET balance = ? WHERE id = ?", [100, 1])
tx.commit()
```

**After (New API):**
```python
tx = db.begin()
result = tx.execute("UPDATE accounts SET balance = ? WHERE id = ?", [100, 1])
if result.ok:
    rows_affected = result.value
    commit_result = tx.commit()
    if not commit_result.ok:
        print("Commit failed: {}".format(commit_result.error))
else:
    print("Execute failed: {}".format(result.error))
    tx.rollback()
```

### Query Operations

**Before (Old API):**
```python
tx = db.begin()
rows = tx.query("SELECT * FROM accounts")
for row in rows:
    print("Account: {}".format(row["name"]))
```

**After (New API):**
```python
tx = db.begin()
result = tx.query("SELECT * FROM accounts")
if result.ok:
    rows = result.value
    for row in rows:
        print("Account: {}".format(row["name"]))
else:
    print("Query failed: {}".format(result.error))
```

### Query One Operations

**Before (Old API):**
```python
tx = db.begin()
account = tx.query_one("SELECT * FROM accounts WHERE id = ?", [1])
if account:
    print("Balance: {}".format(account["balance"]))
```

**After (New API):**
```python
tx = db.begin()
result = tx.query_one("SELECT * FROM accounts WHERE id = ?", [1])
if result.ok:
    account = result.value
    if account:
        print("Balance: {}".format(account["balance"]))
else:
    print("Query failed: {}".format(result.error))
```

### Error Handling Pattern

**New pattern for robust transaction handling:**
```python
def safe_transfer(db, from_id, to_id, amount):
    """Transfer money with comprehensive error handling."""
    tx = db.begin()
    
    # Check balance
    balance_result = tx.query_one("SELECT balance FROM accounts WHERE id = ?", [from_id])
    if not balance_result.ok:
        print("Failed to check balance: {}".format(balance_result.error))
        tx.rollback()
        return False
    
    account = balance_result.value
    if not account or account["balance"] < amount:
        print("Insufficient funds")
        tx.rollback()
        return False
    
    # Perform debit
    debit_result = tx.execute("UPDATE accounts SET balance = balance - ? WHERE id = ?", [amount, from_id])
    if not debit_result.ok:
        print("Debit failed: {}".format(debit_result.error))
        tx.rollback()
        return False
    
    # Perform credit
    credit_result = tx.execute("UPDATE accounts SET balance = balance + ? WHERE id = ?", [amount, to_id])
    if not credit_result.ok:
        print("Credit failed: {}".format(credit_result.error))
        tx.rollback()
        return False
    
    # Commit transaction
    commit_result = tx.commit()
    if not commit_result.ok:
        print("Commit failed: {}".format(commit_result.error))
        return False
    
    return True
```

## OperationResult Properties

Each `OperationResult` has three properties:

- `result.ok` - Boolean indicating success (`True` if no error)
- `result.error` - String containing error message (empty string if no error)
- `result.value` - The operation result value (`None` if error occurred)

## What Hasn't Changed

- `tx.rollback()` still fails immediately on error (as designed)
- Database operations outside transactions work the same way
- Connection and database methods are unchanged

## Key Benefits

1. **Graceful Error Handling**: Scripts don't terminate on database errors
2. **Better Debugging**: Structured error messages for troubleshooting
3. **Transactional Safety**: Proper cleanup and rollback on failures
4. **Consistent API**: All transaction operations follow the same pattern

## Common Patterns

### Check and Use Pattern
```python
result = tx.execute("...")
if result.ok:
    # Use result.value
    pass
else:
    # Handle result.error
    pass
```

### Early Return Pattern
```python
result = tx.execute("...")
if not result.ok:
    print("Error: {}".format(result.error))
    tx.rollback()
    return False
# Continue with result.value
```

### Batch Operations Pattern
```python
tx = db.begin()
for operation in operations:
    result = tx.execute(operation.sql, operation.params)
    if not result.ok:
        print("Failed operation: {}".format(result.error))
        tx.rollback()
        return False

commit_result = tx.commit()
if not commit_result.ok:
    print("Commit failed: {}".format(commit_result.error))
    return False

return True
``` 