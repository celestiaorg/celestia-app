package version

import (
	"fmt"
	"math"
	"sort"
)

// heightRange is a range of heights that a version is valid for. It is an
// internal struct used to search for the correct version given a height.
type heightRange struct {
	start   int64
	end     int64
	version uint64
}

// createRange creates a set of heightRange structs from a map of version
// strings to start heights.
func createRange(versions map[uint64]int64) ([]heightRange, error) {
	ranges := make([]heightRange, 0, len(versions))
	for version, start := range versions {
		ranges = append(ranges, heightRange{
			version: version,
			start:   start,
		})
	}

	sort.Slice(ranges, func(i, j int) bool {
		return ranges[i].start < ranges[j].start
	})

	for i := 0; i < len(ranges)-1; i++ {
		ranges[i].end = ranges[i+1].start - 1
	}

	// set the end of the last range to the max uint64 to cover all heights
	ranges[len(ranges)-1].end = math.MaxInt64

	// validate that the ranges start at 0
	if ranges[0].start != 0 {
		return nil, fmt.Errorf("first version range does not start at 0: %v", ranges)
	}

	return ranges, nil
}

// getVersion returns the app version for a given block height. It performs a
// binary search on the ranges slice.
func getVersion(height int64, ranges []heightRange) uint64 {
	// Perform binary search on the ranges slice
	left := 0
	right := len(ranges) - 1
	for left <= right {
		mid := (left + right) / 2
		if height >= ranges[mid].start && height <= ranges[mid].end {
			return ranges[mid].version
		} else if height < ranges[mid].start {
			right = mid - 1
		} else {
			left = mid + 1
		}
	}
	return ranges[len(ranges)-1].version
}

// VersionGetter stores a set of version ranges and provides a method to
// retrieve the correct version for a given height.
type VersionGetter struct {
	ranges []heightRange
}

// NewVersionGetter creates a new VersionGetter from a map of app versions to starting heights.
func NewVersionGetter(versions map[uint64]int64) (VersionGetter, error) {
	ranges, err := createRange(versions)
	if err != nil {
		return VersionGetter{}, err
	}
	return VersionGetter{
		ranges: ranges,
	}, nil
}

// GetVersion returns the app version for a given height.
func (v VersionGetter) GetVersion(height int64) (appVersion uint64) {
	return getVersion(height, v.ranges)
}
