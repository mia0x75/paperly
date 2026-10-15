package health

import (
	"os"
	"path/filepath"
)

// StorageUsage is the storage usage of the data directory (the data source of
// the home page "storage" card).
//
// Two distinct definitions, not to be mixed:
//
//	AssetsBytes  the recursive size of the <dataDir>/assets directory—**exam
//	              source images**, the data parents produce themselves.
//	              It deliberately does not count the whole dataDir: that also
//	              holds full copies of every past backup under backup/, which
//	              dirSize(dataDir) would double-count, inflating the number with
//	              every backup until parents think they need to clean up.
//
//	DiskTotal / DiskFree / DiskSupported  the capacity of the filesystem
//	              **containing** the data directory, from statfs(dataDir). Asking
//	              "how much space is left on this machine" can only be answered by
//	              the filesystem hosting dataDir—if /data is a separate mount, the
//	              host root partition's free space is irrelevant to this app.
type StorageUsage struct {
	AssetsBytes   int64
	DiskTotal     uint64
	DiskFree      uint64
	DiskSupported bool
}

func MeasureStorage(dir string) StorageUsage {
	usage := StorageUsage{}
	// A nonexistent assets directory is a **normal state** (nothing uploaded on
	// first boot); only a non-NotExist error indicates a real permission or I/O
	// problem.
	if size, err := dirSize(filepath.Join(dir, "assets")); err == nil {
		usage.AssetsBytes = size
	} else if !os.IsNotExist(err) {
		usage.AssetsBytes = size
	}
	if disk, ok := filesystemUsage(dir); ok {
		usage.DiskTotal, usage.DiskFree, usage.DiskSupported = disk.Total, disk.Free, true
	}
	return usage
}

// DiskUsedRatio returns the disk usage ratio 0..1; returns 0 when the platform
// does not support it or capacity cannot be read.
//
// It uses (total-free)/total rather than assets/total: the latter computes "what
// percent of the whole disk your papers occupy", a ratio that is almost always 0
// and tells parents nothing. The real usage ratio is how much of the whole disk
// is used.
func (s StorageUsage) DiskUsedRatio() float64 {
	if !s.DiskSupported || s.DiskTotal == 0 {
		return 0
	}
	return float64(s.DiskTotal-s.DiskFree) / float64(s.DiskTotal)
}

func (s StorageUsage) HumanAssets() string { return HumanBytes(s.AssetsBytes) }
