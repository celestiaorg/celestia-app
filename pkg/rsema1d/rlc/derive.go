package rlc

import (
	"encoding/binary"
	"io"
	"sync"

	"github.com/celestiaorg/celestia-app/v9/pkg/rsema1d/field"
	"github.com/celestiaorg/celestia-app/v9/pkg/rsema1d/merkle"
	"lukechampine.com/blake3"
)

// Derive generates Fiat-Shamir RLC coefficients bound to (rowRoot, k, n, rowSize)
// using BLAKE3's extendable output (XOF). k, n, and rowSize are mixed into the
// seed so coefficients are unique per (rowRoot, k, n, rowSize) tuple. Producer
// paths bind rowSize to the configured row length; consumer paths bind it to the
// actual data length. workers <= 0 is treated as 1; the parallel path only kicks
// in above minParallelSymbols.
//
// Replaces N independent SHA-256 calls (counter-mode PRG) with one BLAKE3 hash
// whose XOF streams numSymbols*32 bytes of pseudo-random output in a single Read
// — the OutputReader internally SIMD-parallelises large reads — followed by a
// cheap per-symbol HashToGF128 conversion that we still fan out for large rows.
func Derive(rowRoot merkle.Root, k, n, rowSize, workers int) Vector {
	h := blake3.New(32, nil)
	h.Write(rowRoot[:])
	var params [12]byte
	binary.LittleEndian.PutUint32(params[0:4], uint32(k))
	binary.LittleEndian.PutUint32(params[4:8], uint32(n))
	binary.LittleEndian.PutUint32(params[8:12], uint32(rowSize))
	h.Write(params[:])

	numSymbols := rowSize / 2 // each GF16 symbol is 2 bytes
	coeffs := make(Vector, numSymbols)

	// One streamed XOF read fills numSymbols*32 bytes of pseudo-random output.
	buf := make([]byte, numSymbols*32)
	if _, err := io.ReadFull(h.XOF(), buf); err != nil {
		panic(err)
	}

	workers = min(max(workers, 1), numSymbols)
	if workers == 1 || numSymbols < minParallelSymbols {
		convertRange(buf, coeffs, 0, numSymbols)
		return coeffs
	}
	convertParallel(buf, coeffs, numSymbols, workers)
	return coeffs
}

func convertParallel(buf []byte, coeffs Vector, numSymbols, workers int) {
	chunk := (numSymbols + workers - 1) / workers
	var wg sync.WaitGroup
	wg.Add(workers)
	for w := range workers {
		start := w * chunk
		end := min(start+chunk, numSymbols)
		go func(start, end int) {
			defer wg.Done()
			convertRange(buf, coeffs, start, end)
		}(start, end)
	}
	wg.Wait()
}

// minParallelSymbols is the empirical break-even on a 24-thread Ryzen:
// parallel is slower below ~256 and 1.3-7× faster from 512 upward.
const minParallelSymbols = 512

// convertRange maps each 32-byte XOF window in buf to a GF128 coefficient.
func convertRange(buf []byte, coeffs Vector, start, end int) {
	for i := start; i < end; i++ {
		coeffs[i] = field.HashToGF128([32]byte(buf[i*32 : i*32+32]))
	}
}
