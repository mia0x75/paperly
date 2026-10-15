//go:build poc

// PoC 2: Autoincrement primary key.
// Verifies INTEGER PRIMARY KEY AUTOINCREMENT: consecutive ids 1/2/3; deleting a middle row never reuses old ids.
// Run: go run ./poc/02_autoincrement.go
package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

func main() {
	tmp, err := os.MkdirTemp("", "poc02-*")
	if err != nil {
		printResult(false, "autoincrement", "failed to create temp dir: %v", err)
		os.Exit(1)
	}
	defer os.RemoveAll(tmp)

	db, err := sql.Open("sqlite", "file:"+filepath.Join(tmp, "poc02.db"))
	if err != nil {
		printResult(false, "autoincrement", "failed to open database: %v", err)
		os.Exit(1)
	}
	defer db.Close()
	ctx := context.Background()

	if _, err := db.ExecContext(ctx, `CREATE TABLE t (id INTEGER PRIMARY KEY AUTOINCREMENT, v TEXT)`); err != nil {
		printResult(false, "autoincrement", "failed to create table: %v", err)
		os.Exit(1)
	}

	insert := func(v string) int64 {
		res, err := db.ExecContext(ctx, `INSERT INTO t (v) VALUES (?)`, v)
		if err != nil {
			printResult(false, "autoincrement", "insert failed: %v", err)
			os.Exit(1)
		}
		id, _ := res.LastInsertId()
		return id
	}

	ids := []int64{insert("a"), insert("b"), insert("c")}
	if ids[0] != 1 || ids[1] != 2 || ids[2] != 3 {
		printResult(false, "autoincrement", "ids not 1/2/3: got=%v", ids)
		os.Exit(1)
	}

	// Delete the middle row (id=2), insert again: the new id must be 4, not reusing 2.
	if _, err := db.ExecContext(ctx, `DELETE FROM t WHERE id = 2`); err != nil {
		printResult(false, "autoincrement", "delete failed: %v", err)
		os.Exit(1)
	}
	newID := insert("d")
	if newID != 4 {
		printResult(false, "autoincrement", "new id did not continue past deletion, got=%d want=4 (old id reused)", newID)
		os.Exit(1)
	}

	printResult(true, "autoincrement", "consecutive ids=%v; after deleting id=2 the new id=%d (not reused)", ids, newID)
}

// printResult emits a uniform result line.
func printResult(pass bool, name, format string, args ...any) {
	tag := "[FAIL]"
	if pass {
		tag = "[PASS]"
	}
	fmt.Printf("%s %s | %s\n", tag, name, fmt.Sprintf(format, args...))
}
