package repository

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMigrateCreatesBaselineTables(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	for _, name := range []string{"users", "kids", "exams", "migrations"} {
		var n int
		require.NoError(t, db.Read().QueryRowContext(ctx,
			`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, name).Scan(&n))
		require.Equal(t, 1, n, "表 %s 应存在", name)
	}
}

func TestMigrateIdempotentOnReopen(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "paperly.db")

	db1, err := Open(ctx, dbPath)
	require.NoError(t, err)
	require.NoError(t, db1.Close())

	db2, err := Open(ctx, dbPath)
	require.NoError(t, err)
	defer func() { require.NoError(t, db2.Close()) }()

	var n int
	require.NoError(t, db2.Read().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM migrations WHERE version = 1`).Scan(&n))
	require.Equal(t, 1, n)
}
