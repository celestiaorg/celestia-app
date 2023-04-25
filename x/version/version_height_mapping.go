package version

import (
	"math"
	"sort"
)

// ChainVersionConfig stores a set of version ranges and provides a method to
// retrieve the correct version for a given height.
type ChainVersionConfig struct {
	Ranges []HeightRange
}

// NewChainVersionConfig creates a new ChainVersionConfig from a map of app versions to starting heights.
func NewChainVersionConfig(versions map[uint64]int64) ChainVersionConfig {
	return ChainVersionConfig{
		Ranges: createRange(versions),
	}
}

// GetVersion returns the app version for a given height.
func (v ChainVersionConfig) GetVersion(height int64) (appVersion uint64) {
	return getVersion(height, v.Ranges)
}

// HeightRange is a range of heights that a version is valid for. It is an
// internal struct used to search for the correct version given a height, and
// should only be created using the createRange function. Heights are
// non-overlapping and inclusive, meaning that the version is valid for all
// heights >= Start and <= End.
type HeightRange struct {
	Start   int64
	End     int64
	Version uint64
}

// createRange creates a set of heightRange structs from a map of version
// strings to start heights.
func createRange(versions map[uint64]int64) []HeightRange {
	ranges := make([]HeightRange, 0, len(versions))
	for version, start := range versions {
		ranges = append(ranges, HeightRange{
			Version: version,
			Start:   start,
		})
	}

	sort.Slice(ranges, func(i, j int) bool {
		return ranges[i].Start < ranges[j].Start
	})

	for i := 0; i < len(ranges)-1; i++ {
		ranges[i].End = ranges[i+1].Start - 1
	}

	// set the end of the last range to the max uint64 to cover all heights
	ranges[len(ranges)-1].End = math.MaxInt64

	return ranges
}

// getVersion returns the app version for a given block height. It performs a
// binary search on the ranges slice.
func getVersion(height int64, ranges []HeightRange) uint64 {
	// Perform binary search on the ranges slice
	left := 0
	right := len(ranges) - 1
	for left <= right {
		mid := (left + right) / 2
		if height >= ranges[mid].Start && height <= ranges[mid].End {
			return ranges[mid].Version
		} else if height < ranges[mid].Start {
			right = mid - 1
		} else {
			left = mid + 1
		}
	}
	return ranges[len(ranges)-1].Version
}
