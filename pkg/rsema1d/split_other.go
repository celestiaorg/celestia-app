//go:build !arm64

package rsema1d

// minSplitRowBytes is the smallest row size (bytes per column) worth splitting.
//
// Off arm64 the baseline Leopard GF16 encode is already fast and keeps the cores
// busy, so a 2-way split of a mid-size blob is pure overhead (extra per-part
// setup and an extra codec work set). The x86 run on AMD Genoa (32 cores) via
// fibre.BenchmarkClient_Upload saw a 16 MiB blob (4096 B/row) regress +61% for a
// single upload and +37%/+18% at concurrency 4/8, while 32 MiB and larger stayed
// a large win (128 MiB single −65%). The floor is therefore one power-of-two
// step above arm64's: 4*minSplitBytes = 8192 B/row, so the first split happens
// at a 32 MiB blob. This removes the mid-size regression and keeps the ≥32 MiB
// and big-blob wins.
const minSplitRowBytes = 4 * minSplitBytes
