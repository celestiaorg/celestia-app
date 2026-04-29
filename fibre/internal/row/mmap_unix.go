//go:build unix

package row

import (
	"fmt"
	"os"
	"runtime"

	"golang.org/x/sys/unix"
)

// disableMmap routes all allocations through the Go heap when FIBRE_ROW_NO_MMAP
// is set. Useful for pprof alloc_space sampling, which doesn't see mmap'd
// pages and would otherwise miss large-slab churn.
var disableMmap = os.Getenv("FIBRE_ROW_NO_MMAP") != ""

// mmapAlloc allocates size bytes via mmap, invisible to Go's GC.
// The returned slice is page-aligned (and therefore SIMD-aligned).
// Caller must call mmapFree when done.
func mmapAlloc(size int) ([]byte, error) {
	pageSize := unix.Getpagesize()
	size = (size + pageSize - 1) &^ (pageSize - 1)

	data, err := unix.Mmap(-1, 0, size,
		unix.PROT_READ|unix.PROT_WRITE,
		unix.MAP_PRIVATE|unix.MAP_ANON)
	if err != nil {
		return nil, fmt.Errorf("mmap(%d): %w", size, err)
	}
	if runtime.GOOS == "linux" {
		// exclude large scratch buffers from core dumps; failure is non-fatal.
		_ = unix.Madvise(data, linuxMadvDontDumpCode)
	}
	return data, nil
}

// mmapFree releases mmap'd memory back to the OS.
func mmapFree(data []byte) error {
	return unix.Munmap(data)
}

// linuxMadvDontDumpCode is the MADV_DONTDUMP advice value on Linux; x/sys
// doesn't export a cross-platform constant since it's Linux-specific.
const linuxMadvDontDumpCode = 16
