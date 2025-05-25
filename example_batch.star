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
    
    print("Setup completed. Results:", setup_results)
    
    # Insert initial data using batch with parameters
    initial_data = db.batch([
        ["INSERT INTO accounts (name, balance) VALUES (?, ?)", ["Alice", 1000.0]],
        ["INSERT INTO accounts (name, balance) VALUES (?, ?)", ["Bob", 500.0]],
        ["INSERT INTO accounts (name, balance) VALUES (?, ?)", ["Charlie", 750.0]]
    ])
    
    print("Initial data inserted. Results:", initial_data)
    
    # Perform a money transfer using batch operations
    transfer_amount = 200.0
    transfer_results = db.batch([
        ["UPDATE accounts SET balance = balance - ? WHERE name = ?", [transfer_amount, "Alice"]],
        ["UPDATE accounts SET balance = balance + ? WHERE name = ?", [transfer_amount, "Bob"]],
        ["INSERT INTO transactions (from_account, to_account, amount) VALUES (?, ?, ?)", [1, 2, transfer_amount]]
    ])
    
    print("Transfer completed. Results:", transfer_results)
    
    # Verify the results
    accounts = db.query("SELECT * FROM accounts ORDER BY name")
    print("Final account balances:")
    for account in accounts:
        print("  {}: ${}".format(account["name"], account["balance"]))
    
    # Check transaction history
    transactions = db.query("SELECT * FROM transactions")
    print("Transaction history:")
    for tx in transactions:
        print("  From account {} to account {}: ${}".format(
            tx["from_account"], tx["to_account"], tx["amount"]))
    
    # Close the connection
    db.close()
    
    print("✓ Batch operations example completed successfully!")

main() 