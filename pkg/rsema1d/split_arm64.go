//go:build arm64

package rsema1d

// minSplitRowBytes is the smallest row size (bytes per column) worth splitting.
//
// On arm64 the baseline Leopard GF16 encode is latency/bandwidth bound and does
// not saturate the cores on its own, so splitting a blob across idle workers
// pays off early. Measured on an Apple M5 Pro via fibre.BenchmarkClient_Upload:
// a single 16 MiB upload (4096 B/row, a 2-way split) is ~48% faster, and every
// larger size improves monotonically (32 MiB −71%, 64 MiB −80%, 128 MiB −83%)
// with no regression at any size or concurrency. So on arm64 the per-part floor
// (minSplitBytes) is the only bound and this gate is a no-op: the smallest
// splittable row is still 2*minSplitBytes = 4096 B (a 16 MiB blob).
const minSplitRowBytes = minSplitBytes
