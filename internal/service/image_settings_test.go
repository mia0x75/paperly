package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"paperly/internal/config"
	"paperly/internal/domain"
	"paperly/internal/storage"
)

func TestImageSettingsDefaultAndClamp(t *testing.T) {
	// An out-of-range initial value falls back to the default tier.
	s := NewImageSettings(99, "")
	require.Equal(t, storage.WebPCompressPreset(config.DefaultImageCompressPreset), s.Preset())

	// A valid value is kept as-is.
	s = NewImageSettings(5, "")
	require.Equal(t, storage.WebPLossyExtreme, s.Preset())
}

func TestImageSettingsSetPersists(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "config.json")
	s := NewImageSettings(3, path)

	require.NoError(t, s.SetImageCompressPreset(ctx, 4))
	require.Equal(t, storage.WebPLossyCompact, s.Preset())

	// Persisted and readable back via Load.
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(raw), `"compress_preset": 4`)

	t.Setenv(config.EnvConfigPath, path)
	t.Setenv(config.EnvDataDir, t.TempDir())
	cfg, err := config.Load()
	require.NoError(t, err)
	require.Equal(t, 4, cfg.Image.CompressPreset)
}

func TestImageSettingsSetRejectsBadValue(t *testing.T) {
	s := NewImageSettings(3, filepath.Join(t.TempDir(), "config.json"))
	err := s.SetImageCompressPreset(context.Background(), 0)
	require.ErrorIs(t, err, domain.ErrInvalidInput)
	require.Equal(t, storage.WebPLossyBalanced, s.Preset(), "非法值不应改动内存档位")
}

func TestImageSettingsSetFailsWithoutPath(t *testing.T) {
	s := NewImageSettings(3, "")
	err := s.SetImageCompressPreset(context.Background(), 5)
	require.Error(t, err)
	require.Equal(t, storage.WebPLossyBalanced, s.Preset(), "写盘失败不应改动内存档位")
}

func TestImageSettingsGet(t *testing.T) {
	s := NewImageSettings(2, "")
	got, err := s.GetImageCompressPreset(context.Background())
	require.NoError(t, err)
	require.Equal(t, 2, got)
}
