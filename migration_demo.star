#!/usr/bin/env starcli

"""
Migration Demo: New OperationResult-based Transaction API

This script demonstrates the enhanced transaction error handling using the new
OperationResult type that allows graceful error handling without script termination.
"""

load("sqlite", "connect")

def main():
    print("🔄 SQLite Transaction Migration Demo")
    print("====================================")
    
    # Connect to in-memory database
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
    db.execute("INSERT INTO accounts (name, balance) VALUES ('Alice', 1000.0)")
    db.execute("INSERT INTO accounts (name, balance) VALUES ('Bob', 500.0)")
    
    print("\n1. Demonstrating successful transaction:")
    success = safe_transfer(db, "Alice", "Bob", 100.0)
    print("   Transfer result: {}".format("SUCCESS" if success else "FAILED"))
    
    print("\n2. Demonstrating error handling (insufficient funds):")
    failure = safe_transfer(db, "Bob", "Alice", 2000.0)
    print("   Transfer result: {}".format("SUCCESS" if failure else "FAILED"))
    
    print("\n3. Demonstrating SQL error handling:")
    error_demo = demo_sql_error(db)
    print("   Error handling result: {}".format("SUCCESS" if error_demo else "FAILED"))
    
    print("\n4. Final account balances:")
    show_balances(db)
    
    db.close()
    print("\n✅ Migration demo completed successfully!")

def safe_transfer(db, from_name, to_name, amount):
    """
    Demonstrates the new OperationResult-based transaction API.
    Returns True if transfer succeeds, False otherwise.
    """
    print("   Attempting to transfer ${} from {} to {}".format(amount, from_name, to_name))
    
    tx = db.begin()
    
    # Step 1: Check sender balance
    balance_result = tx.query_one(
        "SELECT id, balance FROM accounts WHERE name = ?", 
        [from_name]
    )
    
    if not balance_result.ok:
        print("   ❌ Failed to check balance: {}".format(balance_result.error))
        tx.rollback()
        return False
    
    from_account = balance_result.value
    if not from_account:
        print("   ❌ Account '{}' not found".format(from_name))
        tx.rollback()
        return False
    
    if from_account["balance"] < amount:
        print("   ❌ Insufficient funds: ${} available, ${} requested".format(
            from_account["balance"], amount
        ))
        tx.rollback()
        return False
    
    # Step 2: Get recipient account
    to_result = tx.query_one(
        "SELECT id FROM accounts WHERE name = ?", 
        [to_name]
    )
    
    if not to_result.ok:
        print("   ❌ Failed to find recipient: {}".format(to_result.error))
        tx.rollback()
        return False
    
    to_account = to_result.value
    if not to_account:
        print("   ❌ Recipient '{}' not found".format(to_name))
        tx.rollback()
        return False
    
    # Step 3: Perform debit
    debit_result = tx.execute(
        "UPDATE accounts SET balance = balance - ? WHERE id = ?",
        [amount, from_account["id"]]
    )
    
    if not debit_result.ok:
        print("   ❌ Debit failed: {}".format(debit_result.error))
        tx.rollback()
        return False
    
    print("   ✓ Debited ${} from {}".format(amount, from_name))
    
    # Step 4: Perform credit
    credit_result = tx.execute(
        "UPDATE accounts SET balance = balance + ? WHERE id = ?",
        [amount, to_account["id"]]
    )
    
    if not credit_result.ok:
        print("   ❌ Credit failed: {}".format(credit_result.error))
        tx.rollback()
        return False
    
    print("   ✓ Credited ${} to {}".format(amount, to_name))
    
    # Step 5: Commit transaction
    commit_result = tx.commit()
    if not commit_result.ok:
        print("   ❌ Commit failed: {}".format(commit_result.error))
        return False
    
    print("   ✅ Transaction committed successfully")
    return True

def demo_sql_error(db):
    """Demonstrates error handling for SQL errors."""
    print("   Testing SQL error handling...")
    
    tx = db.begin()
    
    # Try to execute invalid SQL
    bad_result = tx.execute("INVALID SQL STATEMENT")
    if bad_result.ok:
        print("   ❌ Expected SQL error but operation succeeded")
        tx.rollback()
        return False
    
    print("   ✓ SQL error caught gracefully: {}".format(bad_result.error[:50] + "..."))
    
    # Transaction should still be usable
    good_result = tx.execute("SELECT COUNT(*) as count FROM accounts")
    if not good_result.ok:
        print("   ❌ Transaction should be usable after error")
        tx.rollback()
        return False
    
    print("   ✓ Transaction remains usable after SQL error")
    tx.rollback()
    return True

def show_balances(db):
    """Display current account balances."""
    accounts = db.query("SELECT name, balance FROM accounts ORDER BY name")
    for account in accounts:
        print("   {}: ${:.2f}".format(account["name"], account["balance"]))

if __name__ == "__main__":
    main() 