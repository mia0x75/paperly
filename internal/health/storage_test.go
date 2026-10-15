package health

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMeasureStorageCountsOnlyAssetsNotBackups(t *testing.T) {
	dir := t.TempDir()
	// Create fake assets and backup directories: the data in backups must NOT be
	// counted toward "how much you uploaded", otherwise the number doubles with
	// each backup.
	writeFile := func(rel string, size int) {
		path := filepath.Join(dir, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, make([]byte, size), 0o600))
	}
	writeFile("assets/2026/09/p1.png", 1000)
	writeFile("assets/notes/p1.txt", 500)
	writeFile("backup/daily/paperly-2026-09-01.db", 9999)

	usage := MeasureStorage(dir)
	require.EqualValues(t, 1500, usage.AssetsBytes, "只统计 assets 子目录")
	require.Equal(t, "1.5 KB", usage.HumanAssets())
}

// A missing assets directory is a normal first-run state and must not make the
// measurement fail or error.
func TestMeasureStorageAssetsDirMissing(t *testing.T) {
	usage := MeasureStorage(t.TempDir())
	require.Zero(t, usage.AssetsBytes)
	require.Equal(t, "0 B", usage.HumanAssets())
}

func TestDiskUsedRatio(t *testing.T) {
	// When the platform does not support statfs (Windows), it must return 0 and
	// mark support as false, letting the frontend hide the usage ratio instead
	// of showing "0.0%" — which would read as "the disk is empty".
	usage := StorageUsage{}
	require.Zero(t, usage.DiskUsedRatio())
	require.False(t, usage.DiskSupported)

	usage = StorageUsage{DiskTotal: 1000, DiskFree: 250, DiskSupported: true}
	require.InDelta(t, 0.75, usage.DiskUsedRatio(), 1e-9)

	// total=0 (statfs reporting an anomalous value) must not divide-by-zero panic.
	usage = StorageUsage{DiskTotal: 0, DiskFree: 0, DiskSupported: true}
	require.Zero(t, usage.DiskUsedRatio())
}
