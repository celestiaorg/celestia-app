package fibre

import "math/bits"

// sizeBucket rounds a byte size up to the next power of two so it can be used
// as a metric attribute without unbounded cardinality. Sizes at or below 0
// return 0.
func sizeBucket(size int64) int64 {
	if size <= 1 {
		return max(size, 0)
	}
	return 1 << bits.Len64(uint64(size-1))
}
