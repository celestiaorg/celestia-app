package rsema1d

import (
	"math/rand/v2"
	"testing"
)

// Encode must not depend on the prior contents of the parity rows.
func TestEncodeIgnoresDirtyParity(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	for _, tc := range []struct{ k, n, rowSize int }{
		{8, 8, 64},
		{32, 96, 128},
		{1024, 1024, 64},
		{4096, 12288, 64},
	} {
		cfg := &Config{K: tc.k, N: tc.n, WorkerCount: 4}
		coder, err := NewCoder(cfg)
		if err != nil {
			t.Fatal(err)
		}
		clean := make([][]byte, tc.k+tc.n)
		dirty := make([][]byte, tc.k+tc.n)
		for i := range clean {
			clean[i] = make([]byte, tc.rowSize)
			dirty[i] = make([]byte, tc.rowSize)
			if i < tc.k {
				for j := range clean[i] {
					clean[i][j] = byte(rng.Uint32())
				}
				copy(dirty[i], clean[i])
			} else {
				for j := range dirty[i] {
					dirty[i][j] = byte(rng.Uint32())
				}
			}
		}
		want, err := coder.Encode(clean)
		if err != nil {
			t.Fatal(err)
		}
		got, err := coder.Encode(dirty)
		if err != nil {
			t.Fatal(err)
		}
		if got.Commitment() != want.Commitment() {
			t.Fatalf("K=%d N=%d: commitment differs with dirty parity", tc.k, tc.n)
		}
		for i := tc.k; i < tc.k+tc.n; i++ {
			if string(dirty[i]) != string(clean[i]) {
				t.Fatalf("K=%d N=%d: parity row %d differs", tc.k, tc.n, i)
			}
		}
	}
}
