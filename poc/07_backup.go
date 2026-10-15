//go:build poc

// PoC 7: Backup (VACUUM INTO).
// Verifies: write 1000 rows -> VACUUM INTO a new file -> open the backup and check data consistency, with the source DB staying online.
// Run: go run ./poc/07_backup.go
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

const backupRows = 1000

func main() {
	tmp, err := os.MkdirTemp("", "poc07-*")
	if err != nil {
		printResult(false, "backup", "failed to create temp dir: %v", err)
		os.Exit(1)
	}
	defer os.RemoveAll(tmp)

	srcPath := filepath.Join(tmp, "src.db")
	backupPath := filepath.Join(tmp, "backup.db")

	src, err := sql.Open("sqlite", "file:"+srcPath+"?_pragma=journal_mode(WAL)")
	if err != nil {
		printResult(false, "backup", "failed to open source db: %v", err)
		os.Exit(1)
	}
	ctx := context.Background()

	if _, err := src.ExecContext(ctx,
		`CREATE TABLE t (id INTEGER PRIMARY KEY AUTOINCREMENT, v TEXT)`); err != nil {
		printResult(false, "backup", "failed to create table: %v", err)
		os.Exit(1)
	}
	tx, err := src.BeginTx(ctx, nil)
	if err != nil {
		printResult(false, "backup", "failed to begin transaction: %v", err)
		os.Exit(1)
	}
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO t (v) VALUES (?)`)
	if err != nil {
		printResult(false, "backup", "failed to prepare statement: %v", err)
		os.Exit(1)
	}
	for i := 0; i < backupRows; i++ {
		if _, err := stmt.ExecContext(ctx, fmt.Sprintf("row-%d", i)); err != nil {
			printResult(false, "backup", "write failed: %v", err)
			os.Exit(1)
		}
	}
	_ = stmt.Close()
	if err := tx.Commit(); err != nil {
		printResult(false, "backup", "commit failed: %v", err)
		os.Exit(1)
	}

	// VACUUM INTO: online backup, the source DB stays up.
	start := time.Now()
	if _, err := src.ExecContext(ctx, `VACUUM INTO ?`, backupPath); err != nil {
		printResult(false, "backup", "VACUUM INTO failed: %v", err)
		os.Exit(1)
	}
	dur := time.Since(start)

	// The source DB is still readable during backup (proves it stayed online).
	var srcCount int
	if err := src.QueryRowContext(ctx, `SELECT COUNT(*) FROM t`).Scan(&srcCount); err != nil {
		printResult(false, "backup", "source read after backup failed: %v", err)
		os.Exit(1)
	}
	_ = src.Close()

	// Open the backup file and verify consistency.
	bk, err := sql.Open("sqlite", "file:"+backupPath)
	if err != nil {
		printResult(false, "backup", "failed to open backup: %v", err)
		os.Exit(1)
	}
	defer bk.Close()

	var bkCount int
	if err := bk.QueryRowContext(ctx, `SELECT COUNT(*) FROM t`).Scan(&bkCount); err != nil {
		printResult(false, "backup", "backup count failed: %v", err)
		os.Exit(1)
	}
	var integrity string
	if err := bk.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&integrity); err != nil {
		printResult(false, "backup", "integrity_check failed: %v", err)
		os.Exit(1)
	}
	fi, err := os.Stat(backupPath)
	if err != nil {
		printResult(false, "backup", "stat backup failed: %v", err)
		os.Exit(1)
	}

	if bkCount != backupRows || srcCount != backupRows {
		printResult(false, "backup", "count mismatch src=%d backup=%d want=%d", srcCount, bkCount, backupRows)
		os.Exit(1)
	}
	if integrity != "ok" {
		printResult(false, "backup", "integrity_check=%s", integrity)
		os.Exit(1)
	}

	printResult(true, "backup", "method=VACUUM INTO; source online; rows=%d match; integrity=%s; backup size=%.1fKB; elapsed=%s",
		bkCount, integrity, float64(fi.Size())/1024, dur.Truncate(time.Millisecond))
}

// printResult emits a uniform result line.
func printResult(pass bool, name, format string, args ...any) {
	tag := "[FAIL]"
	if pass {
		tag = "[PASS]"
	}
	fmt.Printf("%s %s | %s\n", tag, name, fmt.Sprintf(format, args...))
}
