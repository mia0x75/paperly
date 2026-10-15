package repository

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"paperly/internal/domain"
)

// A DATETIME text column must scan into time.Time.
func TestDateScanEmpirical(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := NewKidRepoSQLite(db)

	s := &domain.Kid{
		Name:       "日期测试",
		Grade:      9,
		AccessCode: "dateprob",
		PinVersion: 1,
		Status:     domain.KidStatusActive,
	}
	id, err := repo.Insert(ctx, s)
	require.NoError(t, err)

	got, err := repo.GetByID(ctx, id)
	require.NoError(t, err)
	require.False(t, got.CreatedAt.IsZero(), "DATETIME 列应能扫入 time.Time")
	require.WithinDuration(t, time.Now().UTC(), got.CreatedAt, time.Hour)
}
