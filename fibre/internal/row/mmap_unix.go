//go:build unix

package row

import (
	"fmt"
	"log/slog"
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

// mmapFree releases mmap'd memory off-thread via a single drain
// goroutine. munmap of touched pages costs ~60 µs at 1 MiB and
// parallelizing it regresses throughput, so serializing on one core
// wins. Falls back to inline munmap on full channel.
func mmapFree(data []byte) {
	select {
	case munmapCh <- data:
	default:
		munmap(data)
	}
}

var munmapCh = make(chan []byte, 128)

func init() {
	go munmapDrain()
}

func munmapDrain() {
	for region := range munmapCh {
		munmap(region)
	}
}

func munmap(data []byte) {
	if err := unix.Munmap(data); err != nil {
		slog.Error("row: munmap failed", "size", len(data), "err", err)
	}
}

// linuxMadvDontDumpCode is the MADV_DONTDUMP advice value on Linux; x/sys
// doesn't export a cross-platform constant since it's Linux-specific.
const linuxMadvDontDumpCode = 16
