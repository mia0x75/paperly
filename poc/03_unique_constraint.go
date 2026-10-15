//go:build poc

// PoC 3: Composite unique constraint.
// Verifies UNIQUE(a,b,c) returns "UNIQUE constraint failed" on conflict.
// Scenario mirrors the (owner, ref, seq) uniqueness used across the schema.
// Run: go run ./poc/03_unique_constraint.go
package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

func main() {
	tmp, err := os.MkdirTemp("", "poc03-*")
	if err != nil {
		printResult(false, "unique constraint", "failed to create temp dir: %v", err)
		os.Exit(1)
	}
	defer os.RemoveAll(tmp)

	db, err := sql.Open("sqlite", "file:"+filepath.Join(tmp, "poc03.db"))
	if err != nil {
		printResult(false, "unique constraint", "failed to open database: %v", err)
		os.Exit(1)
	}
	defer db.Close()
	ctx := context.Background()

	const ddl = `
CREATE TABLE question_kp_weights (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    student_id INTEGER NOT NULL,
    question_id INTEGER NOT NULL,
    kp TEXT NOT NULL,
    weight REAL DEFAULT 1.0,
    UNIQUE(student_id, question_id, kp)
);`
	if _, err := db.ExecContext(ctx, ddl); err != nil {
		printResult(false, "unique constraint", "failed to create table: %v", err)
		os.Exit(1)
	}

	const ins = `INSERT INTO question_kp_weights (student_id, question_id, kp) VALUES (?, ?, ?)`
	if _, err := db.ExecContext(ctx, ins, 1, 100, "mechanics"); err != nil {
		printResult(false, "unique constraint", "first insert failed: %v", err)
		os.Exit(1)
	}

	// Re-inserting the same triple must raise a unique-constraint error.
	_, err = db.ExecContext(ctx, ins, 1, 100, "mechanics")
	if err == nil {
		printResult(false, "unique constraint", "duplicate insert did not error")
		os.Exit(1)
	}
	if !strings.Contains(err.Error(), "UNIQUE constraint failed") {
		printResult(false, "unique constraint", "unexpected error message: %v", err)
		os.Exit(1)
	}

	// A different third element must be allowed.
	if _, err := db.ExecContext(ctx, ins, 1, 100, "electricity"); err != nil {
		printResult(false, "unique constraint", "distinct element wrongly rejected: %v", err)
		os.Exit(1)
	}

	printResult(true, "unique constraint", "duplicate (1,100,mechanics) hit 'UNIQUE constraint failed'; a changed element inserts fine")
}

// printResult emits a uniform result line.
func printResult(pass bool, name, format string, args ...any) {
	tag := "[FAIL]"
	if pass {
		tag = "[PASS]"
	}
	fmt.Printf("%s %s | %s\n", tag, name, fmt.Sprintf(format, args...))
}
