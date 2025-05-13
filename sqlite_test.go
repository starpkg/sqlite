package sqlite

import (
	"testing"

	"github.com/starpkg/base"
)

func TestNewModule(t *testing.T) {
	m := NewModule()
	if m == nil {
		t.Fatal("NewModule() returned nil")
	}

	// Test default configuration values
	if m.database.Value() != defaultDatabase {
		t.Errorf("Expected database default to be %q, got %q", defaultDatabase, m.database.Value())
	}
	if m.timeout.Value() != defaultTimeout {
		t.Errorf("Expected timeout default to be %f, got %f", defaultTimeout, m.timeout.Value())
	}
	if m.foreignKeys.Value() != defaultForeignKeys {
		t.Errorf("Expected foreignKeys default to be %t, got %t", defaultForeignKeys, m.foreignKeys.Value())
	}
	if m.journalMode.Value() != defaultJournalMode {
		t.Errorf("Expected journalMode default to be %q, got %q", defaultJournalMode, m.journalMode.Value())
	}
	if m.synchronous.Value() != defaultSynchronous {
		t.Errorf("Expected synchronous default to be %q, got %q", defaultSynchronous, m.synchronous.Value())
	}
	if m.cacheSize.Value() != defaultCacheSize {
		t.Errorf("Expected cacheSize default to be %d, got %d", defaultCacheSize, m.cacheSize.Value())
	}
	if m.busyTimeout.Value() != defaultBusyTimeout {
		t.Errorf("Expected busyTimeout default to be %f, got %f", defaultBusyTimeout, m.busyTimeout.Value())
	}

	// Load module
	dict, err := m.LoadModule()
	if err != nil {
		t.Fatalf("Failed to load module: %v", err)
	}
	if len(dict) != 1 {
		t.Errorf("Expected 1 item in dict, got %d", len(dict))
	}
	if _, ok := dict["sqlite"]; !ok {
		t.Errorf("Expected 'sqlite' key in dict, got %v", dict)
	}
}

func TestRunStarlarkTests(t *testing.T) {
	// Test with base package
	err := base.RunStarlarkTests("sqlite", func() base.ModuleProvider {
		return NewModule()
	})
	if err != nil {
		t.Fatalf("Starlark tests failed: %v", err)
	}
}
