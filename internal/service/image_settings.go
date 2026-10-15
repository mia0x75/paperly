package service

import (
	"context"
	"fmt"
	"sync/atomic"

	"paperly/internal/config"
	"paperly/internal/domain"
	"paperly/internal/storage"
)

// ImageSettings holds the "upload image compression preset" runtime setting that can be adjusted in the UI.
//
// Why an atomic variable instead of reading disk each time: upload is a hot path (up to 50 pages at once),
// and reading config.json per page would turn one upload into 50 file I/Os. It is read once into memory at startup;
// on UI change it writes to disk first and only then updates memory, keeping persisted state and runtime in sync.
type ImageSettings struct {
	preset     atomic.Int64
	configPath string
}

// NewImageSettings constructs the image settings; an out-of-range preset falls back to the balanced tier.
func NewImageSettings(preset int, configPath string) *ImageSettings {
	if config.ValidateImagePreset(preset) != nil {
		preset = config.DefaultImageCompressPreset
	}
	s := &ImageSettings{configPath: configPath}
	s.preset.Store(int64(preset))
	return s
}

// Preset returns the current compression preset.
func (s *ImageSettings) Preset() storage.WebPCompressPreset {
	return storage.WebPCompressPreset(s.preset.Load())
}

// GetImageCompressPreset returns the current preset (read by the settings page).
func (s *ImageSettings) GetImageCompressPreset(context.Context) (int, error) {
	return int(s.preset.Load()), nil
}

// SetImageCompressPreset validates → writes back to config.json → updates memory.
//
// The order must not be reversed: disk is written before memory, so a write failure keeps the runtime value unchanged,
// avoiding the strange inconsistency of the service using a new preset that reverts after a restart.
func (s *ImageSettings) SetImageCompressPreset(_ context.Context, preset int) error {
	if err := config.ValidateImagePreset(preset); err != nil {
		return fmt.Errorf("%w: %v", domain.ErrInvalidInput, err)
	}
	if err := config.SaveImagePreset(s.configPath, preset); err != nil {
		return fmt.Errorf("save image settings: %w", err)
	}
	s.preset.Store(int64(preset))
	return nil
}
