//go:build !arm64

package rsema1d

// minSplitRowBytes is the smallest row worth splitting. Off arm64 a single
// encode is already fast, so splitting only pays off from 8 KiB rows (a 32 MiB
// blob).
const minSplitRowBytes = 4 * minSplitBytes
