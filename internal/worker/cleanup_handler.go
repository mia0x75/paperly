package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"paperly/internal/domain"
)

// kind=4 file cleanup.
// detail: {"dir":"assets/exams/14"}, a directory relative to dataDir.
//
// Idempotent: a missing directory counts as success (os.RemoveAll returns nil for a nonexistent
// path), so reruns are safe. The path check (examDirWithinAssets) is black-and-white: an
// out-of-bounds path fails non-retryably without removing any file.
func handleCleanupStorage(ctx context.Context, deps Deps, task *domain.Task) error {
	var d struct {
		Dir string `json:"dir"`
	}
	if err := json.Unmarshal([]byte(task.Detail), &d); err != nil {
		return nonRetryable(fmt.Errorf("parse cleanup_storage detail: %w", err))
	}
	rel, err := examDirWithinAssets(d.Dir)
	if err != nil {
		return err
	}
	if err := os.RemoveAll(filepath.Join(deps.DataDir, rel)); err != nil {
		// Runtime errors (file in use, permissions): back to the queue per the retry rules.
		return fmt.Errorf("clean directory %s: %w", rel, err)
	}
	return nil
}
