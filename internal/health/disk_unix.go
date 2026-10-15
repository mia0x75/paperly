//go:build !windows

package health

import "syscall"

type DiskUsage struct {
	Total uint64
	Free  uint64
}

// filesystemUsage reads the capacity of the filesystem containing the data
// directory via statfs.
//
// Returning ok=false means the platform does not support it (see
// disk_windows.go); the caller then degrades to "report used only" rather than
// reporting a fabricated total.
func filesystemUsage(dir string) (DiskUsage, bool) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return DiskUsage{}, false
	}
	// Block size × block count. A Blocks overflow is unrealistic even on huge
	// disks (uint64), but Bavail is closer to "the space an unprivileged process
	// can actually use"—and that is what the health page shows parents.
	return DiskUsage{
		Total: st.Blocks * uint64(st.Bsize),
		Free:  st.Bavail * uint64(st.Bsize),
	}, true
}
