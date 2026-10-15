//go:build poc

// PoC 6: Single-file concurrency.
// Compares connection-pool/DSN configs under concurrent read/write from 10 goroutines to pick optimal params.
//
//	A) default sql.Open + default pool: concurrent INSERT, records SQLITE_BUSY error rate
//	B) write pool SetMaxOpenConns(1) + busy_timeout=5000 + _txlock=immediate: concurrent INSERT
//	C) read pool SetMaxOpenConns(10): concurrent SELECT
//
// Pass criteria: B, C error-free; A's error rate recorded as baseline.
// Run: go run ./poc/06_concurrency.go
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

const (
	goroutines = 10
	insertsPer = 200
)

func main() {
	tmp, err := os.MkdirTemp("", "poc06-*")
	if err != nil {
		printResult(false, "concurrency", "failed to create temp dir: %v", err)
		os.Exit(1)
	}
	defer os.RemoveAll(tmp)

	// ---------- Scenario A: default pool, concurrent writes ----------
	aDB, err := sql.Open("sqlite", "file:"+filepath.Join(tmp, "a.db")+"?_pragma=journal_mode(WAL)")
	if err != nil {
		printResult(false, "concurrency", "A open failed: %v", err)
		os.Exit(1)
	}
	setupWriteTable(context.Background(), aDB)
	aBusy, aOther := concurrentInsert(aDB)
	_ = aDB.Close()

	// ---------- Scenario B: write pool (1 conn) + busy_timeout + txlock=immediate ----------
	bDSN := "file:" + filepath.Join(tmp, "b.db") +
		"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_txlock=immediate"
	bDB, err := sql.Open("sqlite", bDSN)
	if err != nil {
		printResult(false, "concurrency", "B open failed: %v", err)
		os.Exit(1)
	}
	bDB.SetMaxOpenConns(1)
	bDB.SetMaxIdleConns(1)
	setupWriteTable(context.Background(), bDB)
	bBusy, bOther := concurrentInsert(bDB)
	_ = bDB.Close()

	// ---------- Scenario C: read pool (10 conns), concurrent reads ----------
	cDB, err := sql.Open("sqlite", "file:"+filepath.Join(tmp, "c.db")+"?_pragma=journal_mode(WAL)")
	if err != nil {
		printResult(false, "concurrency", "C open failed: %v", err)
		os.Exit(1)
	}
	cDB.SetMaxOpenConns(10)
	setupWriteTable(context.Background(), cDB)
	seedRows(context.Background(), cDB, 5000)
	cBusy, cOther := concurrentRead(cDB)
	_ = cDB.Close()

	fmt.Printf("scenarioA(default write):  busy=%d other=%d\n", aBusy, aOther)
	fmt.Printf("scenarioB(write1+immediate): busy=%d other=%d\n", bBusy, bOther)
	fmt.Printf("scenarioC(read10):    busy=%d other=%d\n", cBusy, cOther)

	pass := bBusy == 0 && bOther == 0 && cBusy == 0 && cOther == 0
	if !pass {
		printResult(false, "concurrency", "B/C had errors (B busy=%d other=%d; C busy=%d other=%d)", bBusy, bOther, cBusy, cOther)
		os.Exit(1)
	}
	printResult(true, "concurrency",
		"write pool=SetMaxOpenConns(1)+busy_timeout(5000)+_txlock=immediate; read pool=SetMaxOpenConns(10); scenario A default pool busy=%d (baseline)",
		aBusy)
}

func setupWriteTable(ctx context.Context, db *sql.DB) {
	_, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS t (id INTEGER PRIMARY KEY AUTOINCREMENT, worker INTEGER, n INTEGER)`)
	if err != nil {
		fmt.Printf("[FAIL] concurrency | failed to create table: %v\n", err)
		os.Exit(1)
	}
}

// concurrentInsert runs goroutines writers, each inserting insertsPer rows; returns (busy errors, other errors).
func concurrentInsert(db *sql.DB) (busy, other int64) {
	ctx := context.Background()
	var wg sync.WaitGroup
	for w := 0; w < goroutines; w++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for i := 0; i < insertsPer; i++ {
				_, err := db.ExecContext(ctx, `INSERT INTO t (worker, n) VALUES (?, ?)`, worker, i)
				if err == nil {
					continue
				}
				if strings.Contains(strings.ToUpper(err.Error()), "BUSY") ||
					strings.Contains(strings.ToUpper(err.Error()), "LOCKED") {
					atomic.AddInt64(&busy, 1)
				} else {
					atomic.AddInt64(&other, 1)
				}
			}
		}(w)
	}
	wg.Wait()
	return busy, other
}

func seedRows(ctx context.Context, db *sql.DB, n int) {
	tx, _ := db.BeginTx(ctx, nil)
	stmt, _ := tx.PrepareContext(ctx, `INSERT INTO t (worker, n) VALUES (1, ?)`)
	for i := 0; i < n; i++ {
		_, _ = stmt.ExecContext(ctx, i)
	}
	_ = stmt.Close()
	_ = tx.Commit()
}

// concurrentRead runs goroutines readers over several queries; returns (busy errors, other errors).
func concurrentRead(db *sql.DB) (busy, other int64) {
	ctx := context.Background()
	var wg sync.WaitGroup
	for w := 0; w < goroutines; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				var cnt int
				err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM t`).Scan(&cnt)
				if err == nil {
					continue
				}
				if strings.Contains(strings.ToUpper(err.Error()), "BUSY") ||
					strings.Contains(strings.ToUpper(err.Error()), "LOCKED") {
					atomic.AddInt64(&busy, 1)
				} else {
					atomic.AddInt64(&other, 1)
				}
			}
		}()
	}
	wg.Wait()
	return busy, other
}

// printResult emits a uniform result line.
func printResult(pass bool, name, format string, args ...any) {
	tag := "[FAIL]"
	if pass {
		tag = "[PASS]"
	}
	fmt.Printf("%s %s | %s\n", tag, name, fmt.Sprintf(format, args...))
}
