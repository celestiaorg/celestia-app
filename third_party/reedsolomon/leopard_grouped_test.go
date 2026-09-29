package reedsolomon

import (
	"bytes"
	"math/rand"
	"testing"
)

// TestLeopardGroupedMatchesUngrouped checks the row-grouped FFT and encoder
// IFFT against the stage-ordered originals on dirty work buffers.
func TestLeopardGroupedMatchesUngrouped(t *testing.T) {
	initConstants()
	rng := rand.New(rand.NewSource(21))
	ungrouped := defaultOptions
	ungrouped.useNEON = false
	ungrouped.useSHA3 = false
	for _, m := range []int{1, 2, 4, 8, 16, 32, 64, 128, 256, 1024, 4096} {
		for _, mtrunc := range []int{1, 2, 3, m / 4, m/4 + 1, m / 2, m - 1, m} {
			if mtrunc < 1 || mtrunc > m {
				continue
			}
			const size = 2048
			mk := func() [][]byte {
				w := AllocAligned(m, size)
				for i := range w {
					rng.Read(w[i])
				}
				return w
			}
			clone := func(w [][]byte) [][]byte {
				c := make([][]byte, len(w))
				for i := range w {
					c[i] = bytes.Clone(w[i])
				}
				return c
			}
			// FFT: both variants run on identical random rows.
			a := mk()
			b := clone(a)
			fftDIT(a, mtrunc, m, fftSkew[:], &ungrouped)
			fftDITGrouped(b, mtrunc, m, fftSkew[:], &defaultOptions)
			for i := range a {
				if !bytes.Equal(a[i], b[i]) {
					t.Fatalf("fft m=%d mtrunc=%d row %d differs", m, mtrunc, i)
				}
			}
			// Encoder IFFT: work starts dirty, optionally accumulating into xorRes.
			data := mk()[:mtrunc]
			for _, withXor := range []bool{false, true} {
				wa, wb := mk(), mk()
				var xa, xb [][]byte
				if withXor {
					xa = mk()
					xb = clone(xa)
				}
				ifftDITEncoder(data, mtrunc, wa, xa, m, fftSkew[m-1:], &ungrouped)
				ifftDITEncoderGrouped(data, mtrunc, wb, xb, m, fftSkew[m-1:], &defaultOptions)
				for i := range wa {
					if !bytes.Equal(wa[i], wb[i]) {
						t.Fatalf("ifft m=%d mtrunc=%d xor=%v work row %d differs", m, mtrunc, withXor, i)
					}
				}
				for i := range xa {
					if !bytes.Equal(xa[i], xb[i]) {
						t.Fatalf("ifft m=%d mtrunc=%d xorRes row %d differs", m, mtrunc, i)
					}
				}
			}
		}
	}
}
