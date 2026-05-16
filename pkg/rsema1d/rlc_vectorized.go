package rsema1d

import (
	"crypto/subtle"
	"sync"

	"github.com/celestiaorg/celestia-app/v9/pkg/rsema1d/field"
)

// computeRLCVectorized computes the RLC of len(rows) rows against `coeffs`
// using the vectorized GF(2^16) SIMD kernel. It runs the RLC as an
// outer-product accumulation so the inner loop maps to reedsolomon's scalar
// broadcast kernel. Rows must be equal-sized Leopard chunks; K is padded up to
// a multiple of field.LeopardSymbolsPerChunk when needed.
func computeRLCVectorized(rows [][]byte, coeffs []field.GF128, config *Config) []field.GF128 {
	origK := len(rows)
	if origK == 0 {
		return nil
	}

	rows, K := padToSymbolsPerChunk(rows)
	numChunks := len(rows[0]) / field.LeopardChunkSize
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
	cols := make([]byte, field.LeopardSymbolsPerChunk*stride)

	rowBlocks := k / field.LeopardSymbolsPerChunk
	for c := cStart; c < cEnd; c++ {
		transposeChunk(cols, k, rows, c, rowBlocks)
		for j := range field.LeopardSymbolsPerChunk {
			col := cols[j*stride : (j+1)*stride]
			field.MulSliceXor8(&coeffs[c*field.LeopardSymbolsPerChunk+j], col, &outs)
		}
	}
	return buf
}

// transposeChunk gathers the Leopard chunk at row offset c from each row and
// redistributes it into per-symbol column buffers, each in Leopard format.
func transposeChunk(cols []byte, k int, rows [][]byte, c, rowBlocks int) {
	stride := 2 * k
	rowOff := c * field.LeopardChunkSize
	var block [field.LeopardSymbolsPerChunk * field.LeopardChunkSize]byte
	for rb := range rowBlocks {
		rowBase := rb * field.LeopardSymbolsPerChunk
		for rr := range field.LeopardSymbolsPerChunk {
			copy(block[rr*field.LeopardChunkSize:(rr+1)*field.LeopardChunkSize],
				rows[rowBase+rr][rowOff:rowOff+field.LeopardChunkSize])
		}
		colOff := rb * field.LeopardChunkSize
		for j := range field.LeopardSymbolsPerChunk {
			dst := cols[j*stride+colOff : j*stride+colOff+field.LeopardChunkSize]
			for rr := range field.LeopardSymbolsPerChunk {
				src := block[rr*field.LeopardChunkSize:]
				dst[rr] = src[j]
				dst[field.LeopardSymbolsPerChunk+rr] = src[field.LeopardSymbolsPerChunk+j]
			}
		}
	}
}

// padToSymbolsPerChunk returns rows with its length rounded up to a multiple
// of LeopardSymbolsPerChunk. rows is not mutated; a new slice header is
// returned only when padding is needed, with padded entries sharing one zero row.
func padToSymbolsPerChunk(rows [][]byte) ([][]byte, int) {
	K := len(rows)
	rem := K % field.LeopardSymbolsPerChunk
	if rem == 0 {
		return rows, K
	}
	paddedK := K + field.LeopardSymbolsPerChunk - rem
	padded := make([][]byte, paddedK)
	copy(padded, rows)
	zero := make([]byte, len(rows[0]))
	for i := K; i < paddedK; i++ {
		padded[i] = zero
	}
	return padded, paddedK
}
