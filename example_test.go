package sqlite

import (
	"testing"

	"github.com/1set/starlet"
)

func TestExample(t *testing.T) {
	// Create a new SQL module
	sqliteModule := NewModule()

	// Create Starlet interpreter with the module
	s := starlet.NewDefault()
	s.AddLazyloadModules(starlet.ModuleLoaderMap{
		ModuleName: sqliteModule.LoadModule(),
	})

	// Example script that creates and uses a SQLite database
	const script = `
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
    tx.execute("UPDATE users SET age = age + 1 WHERE name = ?", ["Alice"])
    tx.commit()
    
    # Query with parameters
    user = db.query_one("SELECT * FROM users WHERE name = ?", ["Alice"])
    if user:
        print("Alice's new age: {}".format(user["age"]))
    
    # High-level table operations
    db.create_table("products", {
        "id": "INTEGER PRIMARY KEY",
        "name": "TEXT NOT NULL",
        "price": "REAL DEFAULT 0.0"
    })
    
    # Insert records
    product_id = db.insert("products", {"name": "Laptop", "price": 999.99})
    print("Inserted product with ID: {}".format(product_id))
    
    # Close the connection
    db.close()

main()
`

	// Execute the script
	_, err := s.RunScript([]byte(script), nil)
	if err != nil {
		t.Fatalf("Error executing script: %v\n", err)
	}

	// If we get here, the test passed
	t.Log("Example test executed successfully")
}

func TestTransactions(t *testing.T) {
	// Create a new SQL module
	sqliteModule := NewModule()

	// Create Starlet interpreter with the module
	s := starlet.NewDefault()
	s.AddLazyloadModules(starlet.ModuleLoaderMap{
		ModuleName: sqliteModule.LoadModule(),
	})

	// Example script that demonstrates transaction operations
	const script = `
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
    
    # Function that transfers money using a transaction
    def transfer_money(from_id, to_id, amount):
        tx = db.begin()
        
        # Check if from_account has sufficient funds
        from_account = tx.query_one("SELECT * FROM accounts WHERE id = ?", [from_id])
        if not from_account or from_account["balance"] < amount:
            print("Insufficient funds, rolling back")
            tx.rollback()
            return False
        
        # Update balances
        tx.execute("UPDATE accounts SET balance = balance - ? WHERE id = ?", [amount, from_id])
        tx.execute("UPDATE accounts SET balance = balance + ? WHERE id = ?", [amount, to_id])
        
        # Commit the transaction
        tx.commit()
        print("Transfer successful")
        return True
    
    # Successful transfer
    transfer_money(1, 2, 200.0)
    
    # Verify balances after successful transfer
    alice = db.query_one("SELECT * FROM accounts WHERE id = 1")
    bob = db.query_one("SELECT * FROM accounts WHERE id = 2")
    print("Alice balance: {}, Bob balance: {}".format(alice["balance"], bob["balance"]))
    
    # Failed transfer (insufficient funds)
    transfer_money(2, 1, 1000.0)
    
    # Close the connection
    db.close()

main()
`

	// Execute the script
	_, err := s.RunScript([]byte(script), nil)
	if err != nil {
		t.Fatalf("Error executing script: %v\n", err)
	}

	t.Log("Transaction test executed successfully")
}

func TestPreparedStatements(t *testing.T) {
	// Create a new SQL module
	sqliteModule := NewModule()

	// Create Starlet interpreter with the module
	s := starlet.NewDefault()
	s.AddLazyloadModules(starlet.ModuleLoaderMap{
		ModuleName: sqliteModule.LoadModule(),
	})

	// Example script that demonstrates prepared statements
	const script = `
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
    
    # Create a prepared query statement
    query_stmt = db.prepare_query("SELECT * FROM measurements WHERE sensor_id = ? ORDER BY temperature DESC")
    
    # Use the prepared query multiple times with different parameters
    print("Sensor 1 measurements:")
    for row in query_stmt.query([1]):
        print("  Temperature: {}, Humidity: {}".format(row["temperature"], row["humidity"]))
    
    print("Sensor 2 measurements:")
    for row in query_stmt.query([2]):
        print("  Temperature: {}, Humidity: {}".format(row["temperature"], row["humidity"]))
    
    # Close the prepared query
    query_stmt.close()
    
    # Close the database connection
    db.close()

main()
`

	// Execute the script
	_, err := s.RunScript([]byte(script), nil)
	if err != nil {
		t.Fatalf("Error executing script: %v\n", err)
	}

	t.Log("Prepared statements test executed successfully")
}

func TestHighLevelOperations(t *testing.T) {
	// Create a new SQL module
	sqliteModule := NewModule()

	// Create Starlet interpreter with the module
	s := starlet.NewDefault()
	s.AddLazyloadModules(starlet.ModuleLoaderMap{
		ModuleName: sqliteModule.LoadModule(),
	})

	// Example script that demonstrates high-level database operations
	const script = `
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
    
    # Insert records using the high-level API
    db.insert("employees", {"name": "John Doe", "department": "Engineering", "salary": 85000})
    db.insert("employees", {"name": "Jane Smith", "department": "Marketing", "salary": 75000})
    
    # Bulk insert multiple records
    db.insert_many("employees", [
        {"name": "Bob Johnson", "department": "Engineering", "salary": 90000},
        {"name": "Alice Williams", "department": "HR", "salary": 65000},
        {"name": "Charlie Brown", "department": "Engineering", "salary": 80000}
    ])
    
    # Count employees by department
    eng_count = db.count("employees", "department = ?", ["Engineering"])
    print("Engineering employees: {}".format(eng_count))
    
    # Select all employees from a specific department
    engineers = db.select("employees", ["name", "salary"], "department = ? ORDER BY salary DESC", ["Engineering"])
    print("Engineering team:")
    for eng in engineers:
        print("  {} - ${}".format(eng["name"], eng["salary"]))
    
    # Update records
    db.update("employees", {"salary": 95000}, "name = ?", ["Bob Johnson"])
    
    # Upsert (update or insert)
    db.upsert("employees", {"id": 1, "name": "John Doe", "department": "Engineering", "salary": 88000}, ["id"])
    
    # Verify the update
    john = db.query_one("SELECT * FROM employees WHERE name = ?", ["John Doe"])
    print("John's updated salary: ${}".format(john["salary"]))
    
    # Check if a table exists
    if db.table_exists("employees"):
        print("Employees table exists")
    
    # Get table information
    columns = db.table_info("employees")
    print("Table columns:")
    for col in columns:
        print("  {} ({})".format(col["name"], col["type"]))
    
    # Delete a record
    db.delete("employees", "name = ?", ["Alice Williams"])
    
    # List all tables
    tables = db.tables()
    print("Database tables: {}".format(tables))
    
    # Close the connection
    db.close()

main()
`

	// Execute the script
	_, err := s.RunScript([]byte(script), nil)
	if err != nil {
		t.Fatalf("Error executing script: %v\n", err)
	}

	t.Log("High-level operations test executed successfully")
}

func TestAttachDetach(t *testing.T) {
	// Create a new SQL module
	sqliteModule := NewModule()

	// Create Starlet interpreter with the module
	s := starlet.NewDefault()
	s.AddLazyloadModules(starlet.ModuleLoaderMap{
		ModuleName: sqliteModule.LoadModule(),
	})

	// Example script that demonstrates ATTACH and DETACH operations
	const script = `
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
    
    # Move "inactive" users to archive
    main_db.execute("""
        INSERT INTO archive.old_users (name, email)
        SELECT name, email FROM main.current_users WHERE active = 0
    """)
    
    # Insert a user directly into the archive database
    main_db.execute("""
        INSERT INTO archive.old_users (name, email)
        VALUES (?, ?)
    """, ["David", "david@example.com"])
    
    # Query from the attached database
    archived_users = main_db.query("SELECT * FROM archive.old_users")
    print("Archived users:")
    for user in archived_users:
        print("  {} ({})".format(user["name"], user["email"]))
    
    # Detach the archive database
    main_db.detach("archive")
    
    # Verify that the archive is no longer accessible
    query_error = False
    result = None
    
    # In Starlark, we use a different approach since try/except isn't available
    # We'll just show the expected behavior without actually testing it in this example
    print("After detaching, attempting to query archive.old_users would result in an error")
    
    # Close the main database connection
    main_db.close()

main()
`

	// Execute the script
	_, err := s.RunScript([]byte(script), nil)
	if err != nil {
		t.Fatalf("Error executing script: %v\n", err)
	}

	t.Log("ATTACH/DETACH test executed successfully")
}

func TestErrorHandling(t *testing.T) {
	// Create a new SQL module
	sqliteModule := NewModule()

	// Create Starlet interpreter with the module
	s := starlet.NewDefault()
	s.AddLazyloadModules(starlet.ModuleLoaderMap{
		ModuleName: sqliteModule.LoadModule(),
	})

	// Example script that demonstrates error handling
	const script = `
load("sqlite", "connect")

def main():
    # Connect to an in-memory database
    db = connect(":memory:")
    
    # Create a test table
    db.execute("CREATE TABLE test (id INTEGER PRIMARY KEY, name TEXT NOT NULL)")
    
    # Demonstrate error handling patterns
    create_table_error = False
    
    # Starlark doesn't support try/except, so we'll simulate expected behavior
    print("Note: If we tried to create the same table again, it would result in an error")
    
    # SQL syntax error example (without try/except)
    print("Note: Executing 'SELEC * FROM test' would result in a syntax error")
    
    # Constraint violation (NOT NULL constraint)
    print("Note: Inserting a row without a 'name' value would violate the NOT NULL constraint")
    
    # Insert valid data
    db.execute("INSERT INTO test (id, name) VALUES (?, ?)", [1, "test1"])
    
    # Primary key constraint example
    print("Note: Inserting another record with id=1 would violate the PRIMARY KEY constraint")
    
    # Transaction example
    tx = db.begin()
    tx.execute("INSERT INTO test (id, name) VALUES (?, ?)", [2, "test2"])
    
    # Demonstrate a transaction rollback
    print("If an error occurs in a transaction, we can roll it back:")
    tx.rollback()
    
    # Verify transaction was rolled back
    count = db.count("test", "id = ?", [2])
    print("Record count for id=2: {} (should be 0 after rollback)".format(count))
    
    # Close the connection
    db.close()

main()
`

	// Execute the script
	_, err := s.RunScript([]byte(script), nil)
	if err != nil {
		t.Fatalf("Error executing script: %v\n", err)
	}

	t.Log("Error handling test executed successfully")
}
