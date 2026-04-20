package rsema1d

import (
	"crypto/subtle"
	"sync"

	"github.com/celestiaorg/celestia-app/v9/pkg/rsema1d/field"
)

// The batched RLC over K rows is a matrix multiply over GF(2^16):
// Result[r][k] = Σ_i Rows[r][i] * Coeffs[i][k]. We run it as an outer-product
// accumulate so every inner step is a scalar-broadcast multiply — the shape
// reedsolomon's SIMD kernel handles natively — and fuse the 8 GF128 components
// into one kernel call per transposed column.
const symbolsPerChunk = 32 // GF(2^16) symbols stored per 64-byte Leopard chunk

// computeRLCVectorized computes the RLC of len(rows) rows against `coeffs`
// using the vectorized GF(2^16) SIMD kernel. Rows must be Leopard-sized
// (a positive multiple of chunkSize bytes, equal across rows); K is padded
// up to a multiple of symbolsPerChunk internally when needed.
func computeRLCVectorized(rows [][]byte, coeffs []field.GF128, config *Config) []field.GF128 {
	origK := len(rows)
	if origK == 0 {
		return nil
	}

	rows, K := padToSymbolsPerChunk(rows)
	numChunks := len(rows[0]) / chunkSize
	workers := min(max(config.WorkerCount, 1), numChunks)
	if workers == 1 {
		return field.GF128sFromLeopard(accumulateRLC(rows, coeffs, K, 0, numChunks), K)[:origK]
	}

	partials := make([][]byte, workers)
	step, rem := numChunks/workers, numChunks%workers
	var wg sync.WaitGroup
	wg.Add(workers)
	for w := range workers {
		cStart := w*step + min(w, rem)
		cEnd := cStart + step
		if w < rem {
			cEnd++
		}
		go func() {
			defer wg.Done()
			partials[w] = accumulateRLC(rows, coeffs, K, cStart, cEnd)
		}()
	}
	wg.Wait()

	for _, p := range partials[1:] {
		subtle.XORBytes(partials[0], partials[0], p)
	}
	return field.GF128sFromLeopard(partials[0], K)[:origK]
}

// accumulateRLC processes chunk indices [cStart, cEnd) across every row and
// returns a Leopard GF128-buffer (see field.LeopardGF128BufSize) holding the
// partial RLC sums.
func accumulateRLC(rows [][]byte, coeffs []field.GF128, k, cStart, cEnd int) []byte {
	buf := make([]byte, field.LeopardGF128BufSize(k))
	outs := field.LeopardGF128Views(buf, k)
	stride := 2 * k
	cols := make([]byte, symbolsPerChunk*stride)

	rowBlocks := k / symbolsPerChunk
	for c := cStart; c < cEnd; c++ {
		transposeChunk(cols, k, rows, c, rowBlocks)
		for j := range symbolsPerChunk {
			col := cols[j*stride : (j+1)*stride]
			field.MulSliceXor8(&coeffs[c*symbolsPerChunk+j], col, &outs)
		}
	}
	return buf
}

// transposeChunk gathers the 64-byte Leopard chunk at row offset c from each
// of k rows and redistributes it into symbolsPerChunk column buffers, each of
// stride 2k bytes and itself in Leopard format. Reading a block of 32 rows
// at a time keeps each row's cache line hot while we scatter into the column
// buffers.
func transposeChunk(cols []byte, k int, rows [][]byte, c, rowBlocks int) {
	stride := 2 * k
	rowOff := c * chunkSize
	var block [symbolsPerChunk * chunkSize]byte
	for rb := range rowBlocks {
		rowBase := rb * symbolsPerChunk
		for rr := range symbolsPerChunk {
			copy(block[rr*chunkSize:(rr+1)*chunkSize], rows[rowBase+rr][rowOff:rowOff+chunkSize])
		}
		colOff := rb * chunkSize
		for j := range symbolsPerChunk {
			dst := cols[j*stride+colOff : j*stride+colOff+chunkSize]
			for rr := range symbolsPerChunk {
				src := block[rr*chunkSize:]
				dst[rr] = src[j]                                 // low byte
				dst[symbolsPerChunk+rr] = src[symbolsPerChunk+j] // high byte
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
