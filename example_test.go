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
    count = db.count("products", "")
    if count != 1:
        fail("Expected 1 product, found {}".format(count))
    
    # Close the connection
    db.close()
    
    print("✓ All verifications passed")

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
    eng_count = db.count("employees", "department = ?", ["Engineering"])
    print("Engineering employees: {}".format(eng_count))
    if eng_count != 3:
        fail("Expected 3 Engineering employees, found {}".format(eng_count))
    
    # Select all employees from a specific department
    engineers = db.select("employees", ["name", "salary"], "department = ? ORDER BY salary DESC", ["Engineering"])
    print("Engineering team:")
    
    # Verify engineers are returned in correct order (by descending salary)
    if len(engineers) != 3:
        fail("Expected 3 engineers, got {}".format(len(engineers)))
    
    expected_names = ["Bob Johnson", "John Doe", "Charlie Brown"]
    expected_salaries = [90000, 85000, 80000]
    
    for i, eng in enumerate(engineers):
        print("  {} - ${}".format(eng["name"], eng["salary"]))
        if eng["name"] != expected_names[i]:
            fail("Expected engineer {} to be {}, got {}".format(i, expected_names[i], eng["name"]))
        if eng["salary"] != expected_salaries[i]:
            fail("Expected salary {} to be {}, got {}".format(i, expected_salaries[i], eng["salary"]))
    
    # Update records
    updated_rows = db.update("employees", {"salary": 95000}, "name = ?", ["Bob Johnson"])
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
    deleted_rows = db.delete("employees", "name = ?", ["Alice Williams"])
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
    tx.execute("INSERT INTO test_error (id, name) VALUES (?, ?)", [2, "test2"])
    tx.commit()
    
    # Verify commit worked
    committed = db.query("SELECT * FROM test_error WHERE id = 2")
    if len(committed) != 1:
        fail("Expected committed record to be visible")
    print("Verified commit: {}".format(committed[0]["name"]))
    
    # Test transaction with rollback
    tx2 = db.begin()
    tx2.execute("INSERT INTO test_error (id, name) VALUES (?, ?)", [3, "test3"])
    # Data should be visible within transaction
    in_tx = tx2.query("SELECT * FROM test_error WHERE id = 3")
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
`

	// Execute the script
	_, err := s.RunScript([]byte(script), nil)
	if err != nil {
		t.Fatalf("Error executing script: %v\n", err)
	}

	t.Log("Error handling test executed successfully")
}

func TestSchemaOperations(t *testing.T) {
	// Create a new SQL module
	sqliteModule := NewModule()

	// Create Starlet interpreter with the module
	s := starlet.NewDefault()
	s.AddLazyloadModules(starlet.ModuleLoaderMap{
		ModuleName: sqliteModule.LoadModule(),
	})

	// Example script that demonstrates schema operations: indices, truncate_table, and drop_table
	const script = `
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
`

	// Execute the script
	_, err := s.RunScript([]byte(script), nil)
	if err != nil {
		t.Fatalf("Error executing script: %v\n", err)
	}

	t.Log("Schema operations test executed successfully")
}
