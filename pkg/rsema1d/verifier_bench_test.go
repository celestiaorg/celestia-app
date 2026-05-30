package rsema1d_test

import (
	"math/rand/v2"
	"runtime"
	"testing"

	"github.com/celestiaorg/celestia-app/v9/pkg/rsema1d"
)

// BenchmarkVerifier exercises the upload-side Verifier in two regimes that
// matter operationally: serial single-call latency at the production
// worker shape (WorkerCount=1) and concurrent throughput with a pool of
// NumCPU verifiers. Two shard sizes cover the deployment range — 5 MB
// models a 128 MiB blob across ~100 validators, 51 MB models the same
// blob across only 10 validators (worst-case batch at MaxBlobSize).
func BenchmarkVerifier(b *testing.B) {
	for _, sh := range []struct {
		name    string
		k, n    int
		rowSize int
		batch   int
	}{
		{"shard=5MB/k=4096/n=12288/batch=163", 4096, 12288, 32768, 163},
		{"shard=51MB/k=4096/n=12288/batch=1638", 4096, 12288, 32768, 1638},
	} {
		b.Run(sh.name, func(b *testing.B) {
			benchmarkVerifier(b, sh.k, sh.n, sh.rowSize, sh.batch)
		})
	}
}

func benchmarkVerifier(b *testing.B, k, n, rowSize, batch int) {
	numCPU := runtime.GOMAXPROCS(0)

	data := make([][]byte, k)
	r := rand.New(rand.NewPCG(11, 13))
	for i := range data {
		data[i] = make([]byte, rowSize)
		for j := range data[i] {
			data[i][j] = byte(r.IntN(256))
		}
	}
	encodeCfg := &rsema1d.Config{K: k, N: n, WorkerCount: numCPU}
	ed, commitment, rlcOrig := encodeRows(b, encodeCfg, data)
	proofs := make([]*rsema1d.RowProof, batch)
	for i := range proofs {
		p, err := ed.GenerateRowProof(i)
		if err != nil {
			b.Fatalf("GenerateRowProof(%d): %v", i, err)
		}
		proofs[i] = p
	}

	makeVerifier := func() *rsema1d.Verifier {
		v, err := rsema1d.NewVerifier(&rsema1d.Config{K: k, N: n, WorkerCount: 1})
		if err != nil {
			b.Fatalf("NewVerifier: %v", err)
		}
		return v
	}

	b.Run("serial", func(b *testing.B) {
		v := makeVerifier()
		b.ResetTimer()
		for range b.N {
			if _, err := v.Verify(commitment, proofs, rlcOrig); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("parallel/pool=NumCPU", func(b *testing.B) {
		pool := make(chan *rsema1d.Verifier, numCPU)
		for range numCPU {
			pool <- makeVerifier()
		}
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				v := <-pool
				if _, err := v.Verify(commitment, proofs, rlcOrig); err != nil {
					b.Fatal(err)
				}
				pool <- v
			}
		})
	})

	// shared/* mirrors the upload follow-up pattern: one Verify primes the
	// shared state, then many VerifyShared calls reuse it. This is the path
	// that the coefficient cache is meant to accelerate — VerifyShared no
	// longer pays deriveCoefficients per call.
	b.Run("shared/serial", func(b *testing.B) {
		v := makeVerifier()
		if _, err := v.Verify(commitment, proofs, rlcOrig); err != nil {
			b.Fatalf("priming Verify: %v", err)
		}
		b.ResetTimer()
		for range b.N {
			if err := v.VerifyShared(commitment, proofs); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("shared/parallel/pool=NumCPU", func(b *testing.B) {
		pool := make(chan *rsema1d.Verifier, numCPU)
		for range numCPU {
			v := makeVerifier()
			if _, err := v.Verify(commitment, proofs, rlcOrig); err != nil {
				b.Fatalf("priming Verify: %v", err)
			}
			pool <- v
		}
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				v := <-pool
				if err := v.VerifyShared(commitment, proofs); err != nil {
					b.Fatal(err)
				}
				pool <- v
			}
		})
	})
}
