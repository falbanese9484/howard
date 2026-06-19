package database

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

func NewDatabase(path string) (*sql.DB, error) {
	// TODO(review): WIP/dead code — no callers yet, but pulls cgo sqlite into the binary.
	// Gate behind a branch/build tag or wire session storage in. See notes/review-checklist.md.
	if path == "" {
		return nil, errors.New("database path is required")
	}

	dsn := fmt.Sprintf("file:%s?_foreign_keys=on&_journal_mode=WAL&_busy_timeout=5000",
		path,
	)
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, err
	}

	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0 * time.Second)

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}

	return db, nil
}
