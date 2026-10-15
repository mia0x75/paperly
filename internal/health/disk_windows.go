//go:build windows

package health

type DiskUsage struct {
	Total uint64
	Free  uint64
}

// filesystemUsage does not provide capacity probing on Windows.
//
// Deliberately not patched in with golang.org/x/sys/windows: pulling in a
// dependency with platform APIs just to "show free space" is not worth it
// (KISS). On such a machine the health page degrades to "report used bytes
// only", while the other checks work as usual.
func filesystemUsage(string) (DiskUsage, bool) { return DiskUsage{}, false }
