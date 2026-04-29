//go:build !unix

package row

import "errors"

// disableMmap is forced true on platforms without POSIX mmap (notably
// Windows): alloc then always falls through to the Go-heap path.
const disableMmap = true

// mmapAlloc is never called on non-unix (disableMmap gates the call
// site) but must exist for the package to build.
func mmapAlloc(size int) ([]byte, error) {
	return nil, errors.New("row: mmap not supported on this platform")
}

// mmapFree is unreachable on non-unix since no slab is ever flagged
// mmapped; defined for symmetry with the unix build.
func mmapFree(data []byte) error { return nil }
