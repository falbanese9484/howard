package database

import (
	"path/filepath"
	"testing"
)

func TestNewDatabaseOpensSQLiteWithForeignKeys(t *testing.T) {
	db, err := NewDatabase(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("NewDatabase() error = %v", err)
	}
	defer db.Close()

	var foreignKeys int
	if err := db.QueryRow("PRAGMA foreign_keys").Scan(&foreignKeys); err != nil {
		t.Fatalf("query foreign_keys pragma: %v", err)
	}
	if foreignKeys != 1 {
		t.Fatalf("foreign_keys = %d, want 1", foreignKeys)
	}
}

func TestNewDatabaseRequiresPath(t *testing.T) {
	if _, err := NewDatabase(""); err == nil {
		t.Fatal("NewDatabase(empty) error = nil, want error")
	}
}
