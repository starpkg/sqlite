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
    # Connect to an in-memory database
    db = connect(":memory:")

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
