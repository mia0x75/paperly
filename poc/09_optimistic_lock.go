//go:build poc

// PoC 9: Optimistic versioning (basis of the write-transaction + serial-write-queue model).
// Two goroutines issue UPDATE ... WHERE id=? AND row_version=? from the same row_version concurrently:
// expected one rowsAffected=1 (success) and one rowsAffected=0 (version conflict), with no SQLITE_BUSY.
// Run: go run ./poc/09_optimistic_lock.go
package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	_ "modernc.org/sqlite"
)

func main() {
	tmp, err := os.MkdirTemp("", "poc09-*")
	if err != nil {
		printResult(false, "optimistic versioning", "failed to create temp dir: %v", err)
		os.Exit(1)
	}
	defer os.RemoveAll(tmp)

	// Write-pool params from PoC 6: single connection + busy_timeout + immediate.
	dsn := "file:" + filepath.Join(tmp, "poc09.db") +
		"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_txlock=immediate"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		printResult(false, "optimistic versioning", "failed to open database: %v", err)
		os.Exit(1)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	defer db.Close()
	ctx := context.Background()

	if _, err := db.ExecContext(ctx, `
CREATE TABLE mastery (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    student_id INTEGER NOT NULL,
    raw_score REAL DEFAULT 0,
    row_version INTEGER NOT NULL DEFAULT 1
)`); err != nil {
		printResult(false, "optimistic versioning", "failed to create table: %v", err)
		os.Exit(1)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO mastery (student_id, raw_score, row_version) VALUES (1, 60, 1)`); err != nil {
		printResult(false, "optimistic versioning", "failed to init row: %v", err)
		os.Exit(1)
	}

	// Both goroutines read the same row_version=1 and each try to update.
	const readVersion = 1
	var success, conflict, busy, other int64
	var wg sync.WaitGroup
	start := make(chan struct{})

	for w := 0; w < 2; w++ {
		wg.Add(1)
		go func(score float64) {
			defer wg.Done()
			<-start // launch as simultaneously as possible
			res, err := db.ExecContext(ctx,
				`UPDATE mastery SET raw_score=?, row_version=row_version+1 WHERE student_id=? AND row_version=?`,
				score, 1, readVersion)
			if err != nil {
				msg := strings.ToUpper(err.Error())
				if strings.Contains(msg, "BUSY") || strings.Contains(msg, "LOCKED") {
					atomic.AddInt64(&busy, 1)
				} else {
					atomic.AddInt64(&other, 1)
				}
				return
			}
			n, _ := res.RowsAffected()
			if n == 1 {
				atomic.AddInt64(&success, 1)
			} else {
				atomic.AddInt64(&conflict, 1)
			}
		}(70.0 + float64(w))
	}

	close(start)
	wg.Wait()

	// Following the optimistic-lock convention, the loser rereads the new version and retries once.
	if conflict == 1 {
		res, err := db.ExecContext(ctx,
			`UPDATE mastery SET raw_score=?, row_version=row_version+1 WHERE student_id=? AND row_version=?`,
			72.0, 1, 2)
		if err != nil {
			printResult(false, "optimistic versioning", "retry update failed: %v", err)
			os.Exit(1)
		}
		if n, _ := res.RowsAffected(); n != 1 {
			printResult(false, "optimistic versioning", "retry did not succeed rows=%d", n)
			os.Exit(1)
		}
	}

	var finalVersion int
	var finalScore float64
	if err := db.QueryRowContext(ctx,
		`SELECT row_version, raw_score FROM mastery WHERE student_id=1`).Scan(&finalVersion, &finalScore); err != nil {
		printResult(false, "optimistic versioning", "failed to read final value: %v", err)
		os.Exit(1)
	}

	if busy != 0 || other != 0 {
		printResult(false, "optimistic versioning", "unexpected errors busy=%d other=%d", busy, other)
		os.Exit(1)
	}
	if success != 1 || conflict != 1 {
		printResult(false, "optimistic versioning", "want success=1 conflict=1, got success=%d conflict=%d", success, conflict)
		os.Exit(1)
	}
	// One success + one retry => row_version = 3.
	if finalVersion != 3 {
		printResult(false, "optimistic versioning", "final row_version=%d want=3", finalVersion)
		os.Exit(1)
	}

	printResult(true, "optimistic versioning",
		"concurrent update: success=%d conflict=%d busy=%d; conflict detected via rowsAffected=0; after retry row_version=%d final=%s",
		success, conflict, busy, finalVersion, formatScore(finalScore))
}

func formatScore(f float64) string {
	return fmt.Sprintf("%.1f", f)
}

// printResult emits a uniform result line.
func printResult(pass bool, name, format string, args ...any) {
	tag := "[FAIL]"
	if pass {
		tag = "[PASS]"
	}
	fmt.Printf("%s %s | %s\n", tag, name, fmt.Sprintf(format, args...))
}
