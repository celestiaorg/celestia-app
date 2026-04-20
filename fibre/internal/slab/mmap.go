package slab

import (
	"fmt"
	"os"
	"runtime"
	"syscall"
)

// disableMmap routes all slabs through the Go heap when FIBRE_SLAB_NO_MMAP
// is set. Useful for pprof alloc_space sampling, which doesn't see mmap'd
// pages and would otherwise miss large-slab churn.
var disableMmap = os.Getenv("FIBRE_SLAB_NO_MMAP") != ""

// mmapAlloc allocates size bytes via mmap, invisible to Go's GC.
// The returned slice is page-aligned (and therefore SIMD-aligned).
// caller must call mmapFree when done.
func mmapAlloc(size int) ([]byte, error) {
	pageSize := syscall.Getpagesize()
	size = (size + pageSize - 1) &^ (pageSize - 1)

	data, err := syscall.Mmap(-1, 0, size,
		syscall.PROT_READ|syscall.PROT_WRITE,
		syscall.MAP_PRIVATE|syscall.MAP_ANONYMOUS)
	if err != nil {
		return nil, fmt.Errorf("mmap(%d): %w", size, err)
	}
	if runtime.GOOS == "linux" {
		// exclude large scratch buffers from core dumps; failure is non-fatal.
		_ = syscall.Madvise(data, linuxMadvDontDumpCode)
		// TODO: measure whether MADV_HUGEPAGE improves RS work-buffer throughput
		// without unacceptable RSS/latency regressions before enabling it.
		// _ = syscall.Madvise(data, linuxMadvHugePage)
	}
	return data, nil
}

// mmapFree releases mmap'd memory back to the OS.
func mmapFree(data []byte) error {
	return syscall.Munmap(data)
}

const (
	linuxMadvHugePageCode = 14
	linuxMadvDontDumpCode = 16
)
