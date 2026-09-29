package reedsolomon

import (
	"bytes"
	"math/rand"
	"testing"
)

// TestFormalDerivativeMatchesRef checks the grouped formal derivative
// against the stage-ordered loop for every power-of-two row count.
func TestFormalDerivativeMatchesRef(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	ref := defaultOptions
	ref.useNEON = false
	ref.useSHA3 = false
	for n := 1; n <= 32768; n <<= 1 {
		size := 192
		if n > 4096 {
			size = 64
		}
		a := AllocAligned(n, size)
		b := AllocAligned(n, size)
		for i := range a {
			rng.Read(a[i])
			copy(b[i], a[i])
		}
		formalDerivativeRef(a, n, &ref)
		formalDerivative(b, n, &defaultOptions)
		for i := range a {
			if !bytes.Equal(a[i], b[i]) {
				t.Fatalf("n=%d row %d differs", n, i)
			}
		}
	}
}
