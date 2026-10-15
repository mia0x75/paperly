//go:build poc

// PoC 8: Single-file ceiling.
// Writes roughly 1GB of real data and verifies no anomaly (WAL + many BLOB rows).
// Pass criteria: ≥1GB written with no errors and readable back.
// Run (needs ≥3GB free disk):
//
//	go run ./poc/08_file_size.go
//
// Use --mb=256 to shrink the target volume (debugging).
package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const targetMBDefault = 1024

func main() {
	targetMB := targetMBDefault
	for _, a := range os.Args[1:] {
		if strings.HasPrefix(a, "--mb=") {
			if v, err := strconv.Atoi(strings.TrimPrefix(a, "--mb=")); err == nil && v > 0 {
				targetMB = v
			}
		}
	}

	tmp, err := os.MkdirTemp("", "poc08-*")
	if err != nil {
		printResult(false, "file ceiling", "failed to create temp dir: %v", err)
		os.Exit(1)
	}
	defer os.RemoveAll(tmp)

	dbPath := filepath.Join(tmp, "big.db")
	db, err := sql.Open("sqlite",
		"file:"+dbPath+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)")
	if err != nil {
		printResult(false, "file ceiling", "failed to open database: %v", err)
		os.Exit(1)
	}
	db.SetMaxOpenConns(1)
	defer db.Close()
	ctx := context.Background()

	if _, err := db.ExecContext(ctx,
		`CREATE TABLE payload (id INTEGER PRIMARY KEY AUTOINCREMENT, blob BLOB)`); err != nil {
		printResult(false, "file ceiling", "failed to create table: %v", err)
		os.Exit(1)
	}

	// ~64KB per row, written up to the target volume.
	const rowKB = 64
	blob := make([]byte, rowKB*1024)
	for i := range blob {
		blob[i] = byte(i)
	}
	rows := targetMB * 1024 / rowKB

	start := time.Now()
	batch := 0
	for i := 0; i < rows; i++ {
		if _, err := db.ExecContext(ctx, `INSERT INTO payload (blob) VALUES (?)`, blob); err != nil {
			printResult(false, "file ceiling", "write failed at row %d: %v", i, err)
			os.Exit(1)
		}
		batch++
		if batch%2048 == 0 {
			// Periodic checkpoint to keep the WAL from growing.
			if _, err := db.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
				printResult(false, "file ceiling", "checkpoint failed: %v", err)
				os.Exit(1)
			}
		}
	}
	dur := time.Since(start)

	// Read-back check: row count + one sampled row length.
	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM payload`).Scan(&count); err != nil {
		printResult(false, "file ceiling", "count failed: %v", err)
		os.Exit(1)
	}
	var sampleLen int
	if err := db.QueryRowContext(ctx,
		`SELECT LENGTH(blob) FROM payload WHERE id = ?`, count/2).Scan(&sampleLen); err != nil {
		printResult(false, "file ceiling", "sample read failed: %v", err)
		os.Exit(1)
	}

	// Measure main DB file size after checkpoint.
	if _, err := db.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		printResult(false, "file ceiling", "final checkpoint failed: %v", err)
		os.Exit(1)
	}
	fi, err := os.Stat(dbPath)
	if err != nil {
		printResult(false, "file ceiling", "stat failed: %v", err)
		os.Exit(1)
	}
	sizeGB := float64(fi.Size()) / (1024 * 1024 * 1024)
	targetGB := float64(targetMB) / 1024

	if sizeGB < targetGB*0.95 {
		printResult(false, "file ceiling", "file is only %.2fGB, below target %.2fGB", sizeGB, targetGB)
		os.Exit(1)
	}
	if sampleLen != rowKB*1024 {
		printResult(false, "file ceiling", "sample length=%d want=%d", sampleLen, rowKB*1024)
		os.Exit(1)
	}

	printResult(true, "file ceiling", "wrote %.2fGB without errors; rows=%d; main db=%.2fGB; elapsed=%s",
		targetGB, count, sizeGB, dur.Truncate(time.Second))
}

// printResult emits a uniform result line.
func printResult(pass bool, name, format string, args ...any) {
	tag := "[FAIL]"
	if pass {
		tag = "[PASS]"
	}
	fmt.Printf("%s %s | %s\n", tag, name, fmt.Sprintf(format, args...))
}
