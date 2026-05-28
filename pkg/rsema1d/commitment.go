package rsema1d

import (
	"crypto/sha256"
	"encoding/binary"
	"runtime"
	"sync"

	"github.com/celestiaorg/celestia-app/v9/pkg/rsema1d/field"
)

// deriveCoefficients generates RLC coefficients via Fiat-Shamir (internal).
// k, n, and rowSize are bound into the seed so coefficients are unique per
// (rowRoot, k, n, rowSize) tuple. Callers pick where rowSize comes from:
// producer paths (Encode) pass config.RowSize; consumer paths (Verify, Coder)
// pass the actual data length.
func deriveCoefficients(rowRoot [32]byte, k, n, rowSize int) []field.GF128 {
	// Bind rowRoot and the codec parameters into the Fiat-Shamir seed so
	// coefficients are unique per (rowRoot, k, n, rowSize) tuple.
	h := sha256.New()
	h.Write(rowRoot[:])
	var params [12]byte
	binary.LittleEndian.PutUint32(params[0:4], uint32(k))
	binary.LittleEndian.PutUint32(params[4:8], uint32(n))
	binary.LittleEndian.PutUint32(params[8:12], uint32(rowSize))
	h.Write(params[:])
	var seed [32]byte
	h.Sum(seed[:0])

	numSymbols := rowSize / 2 // Each GF16 symbol is 2 bytes
	coeffs := make([]field.GF128, numSymbols)
	workers := min(runtime.GOMAXPROCS(0), numSymbols)
	if workers <= 1 || numSymbols < minParallelDeriveSymbols {
		deriveCoefficientsRange(seed, coeffs, 0, numSymbols)
		return coeffs
	}
	chunk := (numSymbols + workers - 1) / workers
	var wg sync.WaitGroup
	wg.Add(workers)
	for w := range workers {
		start := w * chunk
		end := min(start+chunk, numSymbols)
		go func(start, end int) {
			defer wg.Done()
			deriveCoefficientsRange(seed, coeffs, start, end)
		}(start, end)
	}
	wg.Wait()
	return coeffs
}

// minParallelDeriveSymbols is the empirical break-even on a 24-thread Ryzen:
// parallel is slower below ~256 and 1.3-7× faster from 512 upward.
const minParallelDeriveSymbols = 512

func deriveCoefficientsRange(seed [32]byte, coeffs []field.GF128, start, end int) {
	var input [32 + 4]byte
	copy(input[:32], seed[:])

	// Reuse a single SHA256 hasher with Reset() between iterations.
	// This avoids re-initializing the digest state from scratch on each call
	// to sha256.Sum256, saving ~12% on coefficient derivation.
	h := sha256.New()
	var digest [32]byte
	for i := start; i < end; i++ {
		binary.LittleEndian.PutUint32(input[32:], uint32(i))
		h.Reset()
		h.Write(input[:])
		h.Sum(digest[:0])
		coeffs[i] = field.HashToGF128(digest[:])
	}
}

// computeRLC computes random linear combination for a row (internal)
func computeRLC(row []byte, coeffs []field.GF128) field.GF128 {
	result := field.Zero()
	numChunks := len(row) / chunkSize

	for c := range numChunks {
		chunk := row[c*chunkSize : (c+1)*chunkSize]
		symbols := extractSymbols(chunk)
		for j, sym := range symbols {
			// result += symbol * coefficient
			symbolIndex := c*32 + j // Overall symbol index in the row
			product := field.Mul128(sym, coeffs[symbolIndex])
			result = field.Add128(result, product)
		}
	}
	return result
}

// extractSymbols extracts GF16 symbols from Leopard-formatted chunk (internal)
// Implements Appendix A.1 from spec
func extractSymbols(chunk []byte) []field.GF16 {
	if len(chunk) != chunkSize {
		panic("extractSymbols requires exactly 64-byte chunk")
	}

	symbols := make([]field.GF16, 32)
	for i := range 32 {
		// Leopard format: bytes 0-31 are low bytes, 32-63 are high bytes
		symbols[i] = field.GF16(chunk[32+i])<<8 | field.GF16(chunk[i])
	}
	return symbols
}
