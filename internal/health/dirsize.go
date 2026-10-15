package health

import (
	"io/fs"
	"os"
	"path/filepath"
)

// dirSize ignores symlinks and unreadable entries: a single file with abnormal
// permissions in the backup/assets directory should not take down the whole
// health page—the part already counted is still returned.
func dirSize(root string) (int64, error) {
	if _, err := os.Stat(root); err != nil {
		return 0, err
	}
	var total int64
	err := filepath.WalkDir(root, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // skip an unreadable single entry, don't fail the whole
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil //nolint:nilerr // same as above
		}
		total += info.Size()
		return nil
	})
	return total, err
}
