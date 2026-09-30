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
			fftDITGrouped(b, mtrunc, m, fftSkew[:], nil, nil, &defaultOptions)
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
				ifftDITEncoderGrouped(data, mtrunc, wb, xb, m, fftSkew[m-1:], false, &defaultOptions)
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

// TestLeopardDecoderGroupedMatchesUngrouped checks the row-grouped decoder
// IFFT and sparse FFT against the stage-ordered originals on dirty buffers.
func TestLeopardDecoderGroupedMatchesUngrouped(t *testing.T) {
	initConstants()
	rng := rand.New(rand.NewSource(23))
	ungrouped := defaultOptions
	ungrouped.useNEON = false
	ungrouped.useSHA3 = false
	for _, m := range []int{1, 2, 4, 8, 16, 32, 64, 128, 256, 1024, 4096, 16384, 32768} {
		for _, mtrunc := range []int{1, 2, 3, m / 4, m/4 + 1, m / 2, m - 1, m} {
			if mtrunc < 1 || mtrunc > m {
				continue
			}
			size := 2048
			if m > 4096 {
				size = 64
			}
			a := AllocAligned(m, size)
			b := AllocAligned(m, size)
			// Rows from zeroFrom on are zero for the reference and left dirty
			// for the grouped decoder, which must not read them.
			zeroFrom := decoderZeroFrom(mtrunc, m, &defaultOptions)
			for i := range a {
				rng.Read(a[i])
				copy(b[i], a[i])
				if i >= zeroFrom {
					clear(a[i])
				}
			}
			ifftDITDecoder(mtrunc, a, m, fftSkew[:], &ungrouped)
			ifftDITDecoderGrouped(mtrunc, b, m, fftSkew[:], &defaultOptions)
			for i := range a {
				if !bytes.Equal(a[i], b[i]) {
					t.Fatalf("ifft m=%d mtrunc=%d zeroFrom=%d row %d differs", m, mtrunc, zeroFrom, i)
				}
			}
			for _, density := range []int{1, 8, 64} {
				var e errorBitfield
				for i := 0; i < m; i += 1 + rng.Intn(density) {
					e.set(i)
				}
				e.prepare(m)
				for i := range a {
					rng.Read(a[i])
					copy(b[i], a[i])
				}
				e.fftDIT(a, mtrunc, m, fftSkew[:], &ungrouped)
				fftDITGrouped(b, mtrunc, m, fftSkew[:], &e, nil, &defaultOptions)
				for i := range a {
					if !bytes.Equal(a[i], b[i]) {
						t.Fatalf("sparse fft m=%d mtrunc=%d density=%d row %d differs", m, mtrunc, density, i)
					}
				}
			}
		}
	}
}

// TestLeopardFusedTopMatchesReference checks encodes that merge the IFFT and
// FFT top stages (m = 4*data, m a power of 4) against the portable encoder.
func TestLeopardFusedTopMatchesReference(t *testing.T) {
	shapes := []struct{ data, parity int }{
		{1, 3}, {1, 4}, {4, 9}, {4, 16}, {16, 33}, {16, 48}, {16, 63}, {16, 64},
		{64, 129}, {64, 200}, {64, 256}, {256, 513}, {256, 768}, {256, 1024},
	}
	rng := rand.New(rand.NewSource(5))
	for _, sh := range shapes {
		if m := ceilPow2(sh.parity); !fuseTopStages(m, sh.data) {
			t.Fatalf("shape %v does not take the fused path", sh)
		}
		const size = 2112
		want := AllocAligned(sh.data+sh.parity, size)
		for i := range want {
			rng.Read(want[i])
		}
		got := make([][]byte, len(want))
		for i := range want {
			got[i] = bytes.Clone(want[i])
		}
		ref, err := New(sh.data, sh.parity, WithLeopardGF16(true), WithNEON(false))
		if err != nil {
			t.Fatal(err)
		}
		enc, err := New(sh.data, sh.parity, WithLeopardGF16(true))
		if err != nil {
			t.Fatal(err)
		}
		if err := ref.Encode(want); err != nil {
			t.Fatal(err)
		}
		if err := enc.Encode(got); err != nil {
			t.Fatal(err)
		}
		for i := range want {
			if !bytes.Equal(want[i], got[i]) {
				t.Fatalf("data=%d parity=%d shard %d differs", sh.data, sh.parity, i)
			}
		}
	}
}
