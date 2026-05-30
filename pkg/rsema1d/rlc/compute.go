package rlc

import (
	"crypto/subtle"
	"sync"

	"github.com/celestiaorg/celestia-app/v9/pkg/rsema1d/field"
	"github.com/klauspost/reedsolomon"
)

// The batched RLC over k rows is a matrix multiply over GF(2^16):
// Result[r][k] = Σ_i Rows[r][i] * Coeffs[i][k]. We run it as an outer-product
// accumulate so every inner step is a scalar-broadcast multiply — the shape
// reedsolomon's SIMD kernel handles natively — and fuse the 8 GF128 components
// into one kernel call per transposed column.

// Compute computes the RLC of len(rows) rows against `coeffs` using the
// vectorized GF(2^16) SIMD kernel. Rows must be Leopard-sized (a positive
// multiple of field.LeopardChunkSize bytes, equal across rows). workers <= 0
// is treated as 1. K is padded up to a multiple of field.LeopardSymbolsPerChunk
// internally when needed; the returned slice still has len(rows) entries.
func Compute(rows [][]byte, coeffs Vector, workers int) Vector {
	origK := len(rows)
	if origK == 0 {
		return nil
	}

	rows, k := padToSymbolsPerChunk(rows)
	numChunks := len(rows[0]) / chunkSize
	workers = min(max(workers, 1), numChunks)
	if workers == 1 {
		return gf128sFromColumns(accumulateRLC(rows, coeffs, k, 0, numChunks), k)[:origK]
	}
	return computeParallel(rows, coeffs, k, origK, numChunks, workers)
}

func computeParallel(rows [][]byte, coeffs Vector, k, origK, numChunks, workers int) Vector {
	// Each worker folds a disjoint span of symbol chunks into its own partial
	// accumulator. The RLC is linear in the chunks, so XOR-ing the partials
	// reconstructs the full result.
	partials := make([][]byte, workers)
	var wg sync.WaitGroup
	wg.Add(workers)
	for w := range workers {
		lo, hi := chunkSpan(numChunks, workers, w)
		go func() {
			defer wg.Done()
			partials[w] = accumulateRLC(rows, coeffs, k, lo, hi)
		}()
	}
	wg.Wait()

	total := partials[0]
	for _, p := range partials[1:] {
		subtle.XORBytes(total, total, p)
	}
	return gf128sFromColumns(total, k)[:origK]
}

// chunkSpan splits numChunks into `workers` contiguous, near-equal spans and
// returns the [lo, hi) span owned by worker w. The first numChunks%workers
// workers each take one extra chunk so the spans differ by at most one.
func chunkSpan(numChunks, workers, w int) (lo, hi int) {
	span, extra := numChunks/workers, numChunks%workers
	lo = w*span + min(w, extra)
	hi = lo + span
	if w < extra {
		hi++
	}
	return lo, hi
}

// accumulateRLC folds symbol chunks [chunkLo, chunkHi) of every row into a
// partial RLC and returns it as a component-major (SoA) buffer: the comp-th
// GF16 component of every row lives in acc[comp*stride:(comp+1)*stride] with
// stride = 2*k. Read it back with gf128sFromColumns.
func accumulateRLC(rows [][]byte, coeffs Vector, k, chunkLo, chunkHi int) []byte {
	stride := 2 * k
	// acc holds GF128Width component columns of stride bytes each; comps aliases
	// each column as a mulXorGF128 destination.
	acc := make([]byte, field.GF128Width*stride)
	var comps [field.GF128Width][]byte
	for i := range comps {
		comps[i] = acc[i*stride : (i+1)*stride]
	}
	cols := make([]byte, symbolsPerChunk*stride)

	for c := chunkLo; c < chunkHi; c++ {
		// Transpose chunk c into per-symbol columns, then fold each column into
		// the accumulator with one fused 8-component mul-XOR.
		transposeChunk(cols, k, rows, c)
		for j := range symbolsPerChunk {
			col := cols[j*stride : (j+1)*stride]
			mulXorGF128(&coeffs[c*symbolsPerChunk+j], col, &comps)
		}
	}
	return acc
}

// ComputeRow computes the RLC of a single Leopard-formatted row using a
// straightforward per-symbol scalar loop. Verification paths that only need
// one row's RLC (e.g. standalone proof verification per SPEC §3.5 case 3)
// use this instead of paying Compute's transpose-and-broadcast setup cost.
//
// row must be a positive multiple of chunkSize bytes; len(coeffs) must be at
// least len(row)/2.
func ComputeRow(row []byte, coeffs Vector) field.GF128 {
	result := field.Zero()
	numChunks := len(row) / chunkSize
	for c := range numChunks {
		for j := range symbolsPerChunk {
			symbolIndex := c*symbolsPerChunk + j
			sym := field.GF16FromLeopard(row, symbolIndex)
			result = field.Add128(result, field.Mul128(sym, coeffs[symbolIndex]))
		}
	}
	return result
}

// mulXorGF128 multiplies the Leopard-formatted GF(2^16) slice `in` by the
// GF128 coefficient `coeff` — treating coeff as 8 broadcast GF16 scalars —
// and XOR-accumulates each component product into accs[k]. Every accs[k]
// must have len(in) bytes, and len(in) must be a multiple of chunkSize.
func mulXorGF128(coeff *field.GF128, in []byte, accs *[field.GF128Width][]byte) {
	var s [field.GF128Width]uint16
	for k, v := range coeff {
		s[k] = uint16(v)
	}
	ll.GF16MulSliceXor8(&s, in, accs)
}

// gf128sFromColumns inverts accumulateRLC's component-major layout into one
// GF128 per row: component comp of row r is the r-th GF16 in the comp-th
// column buf[comp*stride:(comp+1)*stride], with stride = 2*k.
func gf128sFromColumns(buf []byte, k int) Vector {
	out := make(Vector, k)
	stride := 2 * k
	for comp := range field.GF128Width {
		col := buf[comp*stride : (comp+1)*stride]
		for r := range k {
			out[r][comp] = field.GF16FromLeopard(col, r)
		}
	}
	return out
}

// transposeChunk redistributes Leopard chunk c of every row into the per-symbol
// column buffers consumed by the kernel: afterwards column j (the slab
// cols[j*stride:(j+1)*stride], stride = 2*k) holds symbol
// (c*symbolsPerChunk + j) of all k rows, itself in Leopard layout.
//
// A 64-byte Leopard chunk packs 32 low bytes followed by 32 high bytes, so the
// redistribution is a 32×32 transpose applied independently to the low- and
// high-byte planes. We gather one block of 32 rows into a contiguous tile
// before transposing so the strided reads stay within a single hot 2KB buffer
// rather than chasing 32 separate row slices.
func transposeChunk(cols []byte, k int, rows [][]byte, c int) {
	stride := 2 * k
	chunkOff := c * chunkSize
	rowBlocks := k / symbolsPerChunk

	var tile [symbolsPerChunk * chunkSize]byte
	for rb := range rowBlocks {
		firstRow := rb * symbolsPerChunk
		for r := range symbolsPerChunk {
			copy(tile[r*chunkSize:(r+1)*chunkSize], rows[firstRow+r][chunkOff:chunkOff+chunkSize])
		}

		// Scatter the tile into the columns. Column j's 64-byte window for this
		// row block holds 32 low bytes then 32 high bytes, one per row; filling
		// it sequentially keeps the writes within one cache line.
		colOff := rb * chunkSize
		for j := range symbolsPerChunk {
			window := cols[j*stride+colOff : j*stride+colOff+chunkSize]
			for r := range symbolsPerChunk {
				row := tile[r*chunkSize:]
				// Symbol j of row r: low byte, then high byte (Leopard planes).
				window[r] = row[j]
				window[symbolsPerChunk+r] = row[symbolsPerChunk+j]
			}
		}
	}
}

// padToSymbolsPerChunk returns rows with its length rounded up to a multiple
// of symbolsPerChunk by appending a single shared zero row, along with the
// (possibly padded) length. rows is not mutated; a new slice header is
// returned only when padding is needed.
func padToSymbolsPerChunk(rows [][]byte) ([][]byte, int) {
	K := len(rows)
	rem := K % symbolsPerChunk
	if rem == 0 {
		return rows, K
	}
	paddedK := K + symbolsPerChunk - rem
	padded := make([][]byte, paddedK)
	copy(padded, rows)
	zero := make([]byte, len(rows[0]))
	for i := K; i < paddedK; i++ {
		padded[i] = zero
	}
	return padded, paddedK
}

// chunkSize and symbolsPerChunk are short aliases for the Leopard layout
// constants this package leans on heavily.
const (
	chunkSize       = field.LeopardChunkSize       // 64
	symbolsPerChunk = field.LeopardSymbolsPerChunk // 32
)

// ll is the reedsolomon LowLevel handle used by mulXorGF128 to dispatch the
// fused 8-way scalar-broadcast mul-XOR kernel.
var ll reedsolomon.LowLevel
