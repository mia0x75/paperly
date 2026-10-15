//go:build poc

// PoC 5: JSON functions.
// Verifies json_each, JSON_EXTRACT, json_array_length.
// Run: go run ./poc/05_json_functions.go
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
	tmp, err := os.MkdirTemp("", "poc05-*")
	if err != nil {
		printResult(false, "JSON functions", "failed to create temp dir: %v", err)
		os.Exit(1)
	}
	defer os.RemoveAll(tmp)

	db, err := sql.Open("sqlite", "file:"+filepath.Join(tmp, "poc05.db"))
	if err != nil {
		printResult(false, "JSON functions", "failed to open database: %v", err)
		os.Exit(1)
	}
	defer db.Close()
	ctx := context.Background()

	// json_each: expands the array into 3 rows.
	rows, err := db.QueryContext(ctx, `SELECT value FROM json_each('["a","b","c"]')`)
	if err != nil {
		printResult(false, "JSON functions", "json_each failed: %v", err)
		os.Exit(1)
	}
	var values []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			printResult(false, "JSON functions", "json_each scan failed: %v", err)
			os.Exit(1)
		}
		values = append(values, v)
	}
	rows.Close()
	if len(values) != 3 {
		printResult(false, "JSON functions", "json_each rows=%d want=3", len(values))
		os.Exit(1)
	}

	// JSON_EXTRACT: fetch the first element.
	if _, err := db.ExecContext(ctx, `CREATE TABLE t (id INTEGER PRIMARY KEY, tags TEXT DEFAULT '[]')`); err != nil {
		printResult(false, "JSON functions", "failed to create table: %v", err)
		os.Exit(1)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO t (tags) VALUES ('["physics","chemistry","exam"]')`); err != nil {
		printResult(false, "JSON functions", "insert failed: %v", err)
		os.Exit(1)
	}

	var first string
	// Note: JSON_EXTRACT(tags, '$.0') returns NULL under SQLite; the correct syntax is '$[0]'
	// (array elements use brackets; the dot form is deprecated). Uses the correct syntax here.
	if err := db.QueryRowContext(ctx, `SELECT JSON_EXTRACT(tags, '$[0]') FROM t WHERE id = 1`).Scan(&first); err != nil {
		printResult(false, "JSON functions", "JSON_EXTRACT failed: %v", err)
		os.Exit(1)
	}
	if first != "physics" {
		printResult(false, "JSON functions", "JSON_EXTRACT($[0])=%q want=physics", first)
		os.Exit(1)
	}

	// json_array_length: array length.
	var n int
	if err := db.QueryRowContext(ctx, `SELECT json_array_length(tags) FROM t WHERE id = 1`).Scan(&n); err != nil {
		printResult(false, "JSON functions", "json_array_length failed: %v", err)
		os.Exit(1)
	}
	if n != 3 {
		printResult(false, "JSON functions", "json_array_length=%d want=3", n)
		os.Exit(1)
	}

	printResult(true, "JSON functions", "json_each=%v, JSON_EXTRACT($[0])=%s, json_array_length=%d (note: $.0 in the spec is wrong; use $[0])", values, first, n)
}

// printResult emits a uniform result line.
func printResult(pass bool, name, format string, args ...any) {
	tag := "[FAIL]"
	if pass {
		tag = "[PASS]"
	}
	fmt.Printf("%s %s | %s\n", tag, name, fmt.Sprintf(format, args...))
}
