//go:build !linux && !darwin && !windows

package system

// probeAvailableMemory returns 0 on unsupported platforms; Tier() then falls
// back to the conservative pressured tier.
func probeAvailableMemory() uint64 { return 0 }
