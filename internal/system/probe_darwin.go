//go:build darwin

package system

import (
	"os/exec"
	"strconv"
	"strings"
)

// probeAvailableMemory returns total physical memory via `sysctl -n hw.memsize`
// (macOS has no readily readable "available" figure; total is a conservative
// upper bound for a Box app).
func probeAvailableMemory() uint64 {
	out, err := exec.Command("sysctl", "-n", "hw.memsize").Output()
	if err != nil {
		return 0
	}
	v, err := strconv.ParseUint(strings.TrimSpace(string(out)), 10, 64)
	if err != nil {
		return 0
	}
	return v
}
