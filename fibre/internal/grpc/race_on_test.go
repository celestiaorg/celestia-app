//go:build race

package grpc

// raceEnabled reports whether the race detector is on. Under it sync.Pool
// drops items at random, so pooled allocation counts are not stable.
const raceEnabled = true
