//go:build arm64

package rsema1d

// minSplitRowBytes is the smallest row worth splitting. On arm64 a single
// encode does not saturate the cores, so splitting pays off from the smallest
// splittable row (a 16 MiB blob).
const minSplitRowBytes = minSplitBytes
