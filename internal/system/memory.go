// Package system provides low-level process/host introspection: available
// memory tiers and a concurrency gate for memory-sensitive work.
//
// Zero business logic; only used by the upload pipeline to keep low-memory
// hosts (e.g. 128MB VPS) from OOM while leaving high-memory hosts unlimited.
package system

import (
	"sync"
	"time"
)

// MemoryTier is the coarse available-memory class used to size the upload
// gate. Detected once at startup and refreshed on an interval; a failed probe
// falls back to TierPressured (conservative).
type MemoryTier int

const (
	TierTight     MemoryTier = iota // < 512MB available
	TierPressured                   // 512MB .. < 2GB
	TierPlentiful                   // >= 2GB available
)

const (
	memoryRefreshInterval      = 5 * time.Minute
	memAvailableBytesTight     = 512 << 20 // 512MB
	memAvailableBytesPressured = 2 << 30   // 2GB
)

// memoryProbe is the platform probe (defined per-OS in build-tagged files);
// overridden in tests.
var memoryProbe = probeAvailableMemory

type memCache struct {
	mu   sync.RWMutex
	tier MemoryTier
	at   time.Time
}

var mem = &memCache{}

// AvailableMemory returns bytes of currently available memory (approximation).
func AvailableMemory() uint64 {
	if v := memoryProbe(); v > 0 {
		return v
	}
	// Unknown platform / probe failure: report a pressured number so the tier
	// logic stays conservative rather than accidentally unlimited.
	return memAvailableBytesPressured - 1
}

// Tier returns the current memory tier, refreshing at most once per interval.
func Tier() MemoryTier {
	mem.mu.RLock()
	at := mem.at
	mem.mu.RUnlock()
	if time.Since(at) >= memoryRefreshInterval {
		return refreshTier()
	}
	return mem.loadTier()
}

func (c *memCache) loadTier() MemoryTier {
	c.mu.RLock()
	t := c.tier
	c.mu.RUnlock()
	return t
}

func refreshTier() MemoryTier {
	avail := AvailableMemory()
	t := classify(avail)
	mem.mu.Lock()
	mem.tier = t
	mem.at = time.Now()
	mem.mu.Unlock()
	return t
}

func classify(avail uint64) MemoryTier {
	switch {
	case avail < memAvailableBytesTight:
		return TierTight
	case avail < memAvailableBytesPressured:
		return TierPressured
	default:
		return TierPlentiful
	}
}
