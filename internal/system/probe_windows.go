//go:build windows

package system

import (
	"syscall"
	"unsafe"
)

type memoryStatusEx struct {
	Length               uint32
	MemoryLoad           uint32
	TotalPhys            uint64
	AvailPhys            uint64
	TotalPageFile        uint64
	AvailPageFile        uint64
	TotalVirtual         uint64
	AvailVirtual         uint64
	AvailExtendedVirtual uint64
}

// probeAvailableMemory returns available physical memory via
// GlobalMemoryStatusEx (Windows).
func probeAvailableMemory() uint64 {
	k32 := syscall.NewLazyDLL("kernel32.dll")
	proc := k32.NewProc("GlobalMemoryStatusEx")
	ms := &memoryStatusEx{Length: uint32(unsafe.Sizeof(memoryStatusEx{}))}
	r, _, _ := proc.Call(uintptr(unsafe.Pointer(ms)))
	if r == 0 {
		return 0
	}
	if ms.AvailPhys > 0 {
		return ms.AvailPhys
	}
	return ms.TotalPhys
}
