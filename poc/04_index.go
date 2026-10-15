//go:build poc

// PoC 4: Indexes and query plans.
// Verifies plain / composite / partial indexes, with EXPLAIN QUERY PLAN showing USING INDEX (no SCAN TABLE).
// Run: go run ./poc/04_index.go
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
	tmp, err := os.MkdirTemp("", "poc04-*")
	if err != nil {
		printResult(false, "indexes", "failed to create temp dir: %v", err)
		os.Exit(1)
	}
	defer os.RemoveAll(tmp)

	db, err := sql.Open("sqlite", "file:"+filepath.Join(tmp, "poc04.db"))
	if err != nil {
		printResult(false, "indexes", "failed to open database: %v", err)
		os.Exit(1)
	}
	defer db.Close()
	ctx := context.Background()

	const ddl = `
CREATE TABLE papers (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    subject TEXT,
    grade TEXT,
    status TEXT DEFAULT 'active',
    exam_date DATE
);
CREATE INDEX idx_papers_subject ON papers(subject);
CREATE INDEX idx_papers_subject_grade ON papers(subject, grade);
CREATE INDEX idx_papers_active_date ON papers(exam_date) WHERE status = 'active';`
	if _, err := db.ExecContext(ctx, ddl); err != nil {
		printResult(false, "indexes", "failed to create table/indexes: %v", err)
		os.Exit(1)
	}

	// Seed data so the optimizer prefers the indexes.
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		printResult(false, "indexes", "failed to begin transaction: %v", err)
		os.Exit(1)
	}
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO papers (subject, grade, status, exam_date) VALUES (?, ?, ?, ?)`)
	if err != nil {
		printResult(false, "indexes", "failed to prepare statement: %v", err)
		os.Exit(1)
	}
	for i := 0; i < 2000; i++ {
		subj := "physics"
		if i%2 == 0 {
			subj = "chemistry"
		}
		if _, err := stmt.ExecContext(ctx, subj, "grade9", "active", "2026-01-01"); err != nil {
			printResult(false, "indexes", "insert failed: %v", err)
			os.Exit(1)
		}
	}
	_ = stmt.Close()
	if err := tx.Commit(); err != nil {
		printResult(false, "indexes", "commit failed: %v", err)
		os.Exit(1)
	}
	if _, err := db.ExecContext(ctx, `ANALYZE`); err != nil {
		printResult(false, "indexes", "ANALYZE failed: %v", err)
		os.Exit(1)
	}

	// Three queries verify the plain / composite / partial indexes are each selected.
	cases := []struct {
		name  string
		query string
	}{
		{"plain", `SELECT id FROM papers WHERE subject = 'physics'`},
		{"composite", `SELECT id FROM papers WHERE subject = 'physics' AND grade = 'grade9'`},
		{"partial", `SELECT id FROM papers WHERE exam_date = '2026-01-01' AND status = 'active'`},
	}

	var plans []string
	for _, tc := range cases {
		plan, err := queryPlan(ctx, db, tc.query)
		if err != nil {
			printResult(false, "indexes", "%s EXPLAIN failed: %v", tc.name, err)
			os.Exit(1)
		}
		if strings.Contains(plan, "SCAN papers") {
			printResult(false, "indexes", "%s did a full table scan: %s", tc.name, plan)
			os.Exit(1)
		}
		if !strings.Contains(plan, "USING") {
			printResult(false, "indexes", "%s plan lacks USING: %s", tc.name, plan)
			os.Exit(1)
		}
		plans = append(plans, tc.name+"="+plan)
	}

	printResult(true, "indexes", "plain/partial/composite all USING INDEX | %s", strings.Join(plans, "; "))
}

// queryPlan returns the concatenated EXPLAIN QUERY PLAN text for string checks.
func queryPlan(ctx context.Context, db *sql.DB, query string) (string, error) {
	rows, err := db.QueryContext(ctx, "EXPLAIN QUERY PLAN "+query)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	var parts []string
	for rows.Next() {
		var id, parent, notUsed int
		var detail string
		if err := rows.Scan(&id, &parent, &notUsed, &detail); err != nil {
			return "", err
		}
		parts = append(parts, detail)
	}
	return strings.Join(parts, " | "), rows.Err()
}

// printResult emits a uniform result line.
func printResult(pass bool, name, format string, args ...any) {
	tag := "[FAIL]"
	if pass {
		tag = "[PASS]"
	}
	fmt.Printf("%s %s | %s\n", tag, name, fmt.Sprintf(format, args...))
}
