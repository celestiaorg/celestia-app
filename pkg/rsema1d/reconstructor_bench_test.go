package rsema1d_test

import (
	"math/rand/v2"
	"testing"

	"github.com/celestiaorg/celestia-app/v10/pkg/rsema1d"
)

// BenchmarkReconstructorFibreMaxReconstruct models the download flow at the
// fibre v0 shape (K=4096, N=12288, rowSize=32KiB): proofs arrive in shard-sized
// batches, so Add fires many times against the same commitment. The first Add
// primes Verify; the rest go through VerifyShared and reuse the cached
// coefficients. Each iteration ends with a full RS reconstruct.
func BenchmarkReconstructorFibreMaxReconstruct(b *testing.B) {
	const (
		k       = 4096
		n       = 12288
		rowSize = 32768
		// chunkSize=163 mirrors the 5MB shard batch from BenchmarkVerifier
		// (128 MiB blob across ~100 validators), so the first Add primes
		// Verify and the remaining ~24 chunks go through VerifyShared.
		chunkSize = 163
	)

	cfg := &rsema1d.Config{K: k, N: n, WorkerCount: 1}
	source := make([][]byte, k)
	for i := range source {
		source[i] = make([]byte, rowSize)
		for j := range source[i] {
			source[i][j] = byte(i + j)
		}
	}

	extData, commitment, _ := encodeRows(b, cfg, source)

	coder, err := rsema1d.NewCoder(&rsema1d.Config{K: k, N: n, WorkerCount: 1})
	if err != nil {
		b.Fatal(err)
	}

	patterns := []struct {
		name    string
		present []bool
	}{
		{
			name:    "first_quarter_originals_rest_parity",
			present: fibreMaxPresentQuarterOriginals(k, n),
		},
		{
			name:    "random_k_rows",
			present: fibreMaxPresentRandom(k, n),
		},
	}

	for _, pattern := range patterns {
		b.Run(pattern.name, func(b *testing.B) {
			b.SetBytes(k * rowSize)

			rows := make([][]byte, k+n)
			originalScratch := make([][]byte, k)
			for i := range originalScratch {
				originalScratch[i] = make([]byte, 0, rowSize)
			}
			resetRows := func() {
				for i := range rows {
					switch {
					case pattern.present[i]:
						rows[i] = extData.Row(i)
					case i < k:
						rows[i] = originalScratch[i][:0]
					default:
						rows[i] = nil
					}
				}
			}

			proofs := make([]*rsema1d.RowProof, 0, k)
			for i, ok := range pattern.present {
				if !ok {
					continue
				}
				proof, err := extData.GenerateRowProof(i)
				if err != nil {
					b.Fatal(err)
				}
				proofs = append(proofs, proof)
			}
			if len(proofs) != k {
				b.Fatalf("expected %d proofs, got %d", k, len(proofs))
			}
			rlc := extData.RLC()

			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				b.StopTimer()
				rec, err := coder.NewReconstructor(commitment)
				if err != nil {
					b.Fatal(err)
				}
				resetRows()
				b.StartTimer()
				for i := 0; i < len(proofs); i += chunkSize {
					end := min(i+chunkSize, len(proofs))
					if _, err := rec.Add(proofs[i:end], rlc); err != nil {
						b.Fatal(err)
					}
				}
				if err := rec.Reconstruct(rows); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func fibreMaxPresentQuarterOriginals(k, n int) []bool {
	present := make([]bool, k+n)
	presentData := k / 4
	for i := range presentData {
		present[i] = true
	}
	for i := k; i < k+(k-presentData); i++ {
		present[i] = true
	}
	return present
}

func fibreMaxPresentRandom(k, n int) []bool {
	present := make([]bool, k+n)
	indices := make([]int, k+n)
	for i := range indices {
		indices[i] = i
	}
	rng := rand.New(rand.NewPCG(1, 2))
	rng.Shuffle(len(indices), func(i, j int) {
		indices[i], indices[j] = indices[j], indices[i]
	})
	for _, idx := range indices[:k] {
		present[idx] = true
	}
	return present
}
