package rlc

import (
	"crypto/sha256"
	"encoding/binary"
	"sync"

	"github.com/celestiaorg/celestia-app/v9/pkg/rsema1d/field"
	"github.com/celestiaorg/celestia-app/v9/pkg/rsema1d/merkle"
)

// Derive generates Fiat-Shamir RLC coefficients bound to (rowRoot, k, n, rowSize).
// k, n, and rowSize are mixed into the seed so coefficients are unique per
// (rowRoot, k, n, rowSize) tuple. Producer paths bind rowSize to the configured
// row length; consumer paths bind it to the actual data length. workers <= 0
// is treated as 1; the parallel path only kicks in above minParallelSymbols.
func Derive(rowRoot merkle.Root, k, n, rowSize, workers int) Vector {
	h := sha256.New()
	h.Write(rowRoot[:])
	var params [12]byte
	binary.LittleEndian.PutUint32(params[0:4], uint32(k))
	binary.LittleEndian.PutUint32(params[4:8], uint32(n))
	binary.LittleEndian.PutUint32(params[8:12], uint32(rowSize))
	h.Write(params[:])
	var seed [32]byte
	h.Sum(seed[:0])

	numSymbols := rowSize / 2 // each GF16 symbol is 2 bytes
	coeffs := make(Vector, numSymbols)
	workers = min(max(workers, 1), numSymbols)
	if workers == 1 || numSymbols < minParallelSymbols {
		deriveRange(seed, coeffs, 0, numSymbols)
		return coeffs
	}
	deriveParallel(seed, coeffs, numSymbols, workers)
	return coeffs
}

func deriveParallel(seed [32]byte, coeffs Vector, numSymbols, workers int) {
	chunk := (numSymbols + workers - 1) / workers
	var wg sync.WaitGroup
	wg.Add(workers)
	for w := range workers {
		start := w * chunk
		end := min(start+chunk, numSymbols)
		go func(start, end int) {
			defer wg.Done()
			deriveRange(seed, coeffs, start, end)
		}(start, end)
	}
	wg.Wait()
}

// minParallelSymbols is the empirical break-even on a 24-thread Ryzen:
// parallel is slower below ~256 and 1.3-7× faster from 512 upward.
const minParallelSymbols = 512

// deriveRange fills coeffs[start:end] by hashing (seed, index) per symbol.
// The hasher is reused across iterations via Reset() to save ~12% on derivation.
func deriveRange(seed [32]byte, coeffs Vector, start, end int) {
	var input [32 + 4]byte
	copy(input[:32], seed[:])

	h := sha256.New()
	var digest [32]byte
	for i := start; i < end; i++ {
		binary.LittleEndian.PutUint32(input[32:], uint32(i))
		h.Reset()
		h.Write(input[:])
		h.Sum(digest[:0])
		coeffs[i] = field.HashToGF128(digest)
	}
}
