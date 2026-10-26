//go:build !arm64 || noasm || nopshufb || race

package rlc

// asmBuild reports that this build cannot take the PMULL assembly path: it is
// non-arm64, or built with -race, -tags noasm, or -tags nopshufb.
const asmBuild = false

// asmPathActive reports whether Compute will run the PMULL kernels at runtime.
func asmPathActive() bool { return false }

// computeFast is the no-op fallback on builds without the PMULL kernels; Compute
// then uses the portable transpose path.
func computeFast([][]byte, Vector, int) (Vector, bool) { return nil, false }
