package rsema1d

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"testing"

	"github.com/celestiaorg/celestia-app/v9/pkg/rsema1d/field"
	"github.com/celestiaorg/celestia-app/v9/pkg/rsema1d/merkle"
)

// BenchmarkVerifyBreakdown isolates each component of VerifyRowWithContext
// to identify where time is actually spent.
func BenchmarkVerifyBreakdown(b *testing.B) {
	for _, tc := range []struct {
		name    string
		rowSize int
		k, n    int
	}{
		{"1MB/k=1024", 1024, 1024, 1024},   // 1 MB blob: 1024 rows of 1024 bytes
		{"8MB/k=1024", 8192, 1024, 1024},    // 8 MB blob: 1024 rows of 8192 bytes
		{"128MB/k=1024", 131072, 1024, 3072}, // 128 MB blob: 1024 rows of 128KB
	} {
		config := &Config{
			K:       tc.k,
			N:       tc.n,
			RowSize: tc.rowSize,
		}

		// Generate a random row
		row := make([]byte, tc.rowSize)
		rand.Read(row)

		// Generate a fake merkle proof (correct depth)
		kPadded := nextPowerOfTwo(tc.k)
		totalPadded := nextPowerOfTwo(kPadded + tc.n)
		depth := 0
		for v := totalPadded; v > 1; v /= 2 {
			depth++
		}
		proof := make([][]byte, depth)
		for i := range proof {
			proof[i] = make([]byte, 32)
			rand.Read(proof[i])
		}

		// Pre-compute row root (from hashing the row as leaf + proof traversal)
		rowRoot := sha256.Sum256(row)

		// Pre-derive coefficients
		coeffs := deriveCoefficients(rowRoot, config)

		b.Run(tc.name+"/ComputeRootFromProof", func(b *testing.B) {
			for b.Loop() {
				merkle.ComputeRootFromProof(row, 0, proof)
			}
		})

		b.Run(tc.name+"/deriveCoefficients", func(b *testing.B) {
			for b.Loop() {
				deriveCoefficients(rowRoot, config)
			}
		})

		b.Run(tc.name+"/computeRLC", func(b *testing.B) {
			for b.Loop() {
				computeRLC(row, coeffs)
			}
		})

		b.Run(tc.name+"/commitmentHash", func(b *testing.B) {
			var rlcOrigRoot [32]byte
			for b.Loop() {
				h := sha256.Sum256(append(rowRoot[:], rlcOrigRoot[:]...))
				_ = h
			}
		})

		// Sub-components
		numSymbols := tc.rowSize / 2

		b.Run(tc.name+"/field.Mul128_x_all_symbols", func(b *testing.B) {
			sym := field.GF16(0x1234)
			coeff := coeffs[0]
			for b.Loop() {
				var result field.GF128
				for range numSymbols {
					product := field.Mul128(sym, coeff)
					result = field.Add128(result, product)
				}
				_ = result
			}
		})

		b.Run(tc.name+"/SHA256_x_all_symbols", func(b *testing.B) {
			var input [36]byte
			copy(input[:32], rowRoot[:])
			for b.Loop() {
				for i := range numSymbols {
					binary.LittleEndian.PutUint32(input[32:], uint32(i))
					sha256.Sum256(input[:])
				}
			}
		})

		b.Run(tc.name+"/SHA256_reuse_x_all_symbols", func(b *testing.B) {
			var input [36]byte
			copy(input[:32], rowRoot[:])
			h := sha256.New()
			var digest [32]byte
			for b.Loop() {
				for i := range numSymbols {
					binary.LittleEndian.PutUint32(input[32:], uint32(i))
					h.Reset()
					h.Write(input[:])
					h.Sum(digest[:0])
				}
			}
		})
	}
}
