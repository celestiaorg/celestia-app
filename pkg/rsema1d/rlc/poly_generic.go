//go:build !arm64 || noasm || nopshufb || race

package rlc

func computeFast([][]byte, Vector, int) (Vector, bool) { return nil, false }
