//go:build poc

// PoC 1: SQL dialect.
// Verifies CREATE TABLE / INTEGER PRIMARY KEY AUTOINCREMENT / TEXT / DATETIME DEFAULT CURRENT_TIMESTAMP.
// Run: go run ./poc/01_sql_dialect.go
package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

func main() {
	tmp, err := os.MkdirTemp("", "poc01-*")
	if err != nil {
		printResult(false, "SQL dialect", "failed to create temp dir: %v", err)
		os.Exit(1)
	}
	defer os.RemoveAll(tmp)

	dsn := "file:" + filepath.Join(tmp, "poc01.db") + "?_pragma=journal_mode(WAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		printResult(false, "SQL dialect", "failed to open database: %v", err)
		os.Exit(1)
	}
	defer db.Close()

	ctx := context.Background()

	// Covers the dialect elements used across the schema.
	const ddl = `
CREATE TABLE poc_dialect (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    age INTEGER DEFAULT 0,
    birthday DATE,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    tags TEXT DEFAULT '[]'
);`
	if _, err := db.ExecContext(ctx, ddl); err != nil {
		printResult(false, "SQL dialect", "CREATE TABLE failed: %v", err)
		os.Exit(1)
	}

	res, err := db.ExecContext(ctx, `INSERT INTO poc_dialect (name, age) VALUES (?, ?)`, "Zhang San", 15)
	if err != nil {
		printResult(false, "SQL dialect", "INSERT failed: %v", err)
		os.Exit(1)
	}
	id, _ := res.LastInsertId()

	var (
		gotName    string
		gotCreated time.Time
	)
	err = db.QueryRowContext(ctx,
		`SELECT name, created_at FROM poc_dialect WHERE id = ?`, id,
	).Scan(&gotName, &gotCreated)
	if err != nil {
		printResult(false, "SQL dialect", "SELECT failed: %v", err)
		os.Exit(1)
	}
	if gotName != "Zhang San" {
		printResult(false, "SQL dialect", "unexpected value: got=%q", gotName)
		os.Exit(1)
	}
	if gotCreated.IsZero() {
		printResult(false, "SQL dialect", "CURRENT_TIMESTAMP not applied")
		os.Exit(1)
	}

	printResult(true, "SQL dialect", "CREATE/INSERT/SELECT ok, DATETIME DEFAULT CURRENT_TIMESTAMP=%s", gotCreated.Format(time.RFC3339))
}

// printResult emits a uniform line: [PASS] name | value | note, or [FAIL] name | reason.
func printResult(pass bool, name, format string, args ...any) {
	tag := "[FAIL]"
	if pass {
		tag = "[PASS]"
	}
	fmt.Printf("%s %s | %s\n", tag, name, fmt.Sprintf(format, args...))
}
