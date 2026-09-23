//go:build !noasm && !appengine && !gccgo && !nopshufb

package reedsolomon

import (
	"bytes"
	"fmt"
	"math/rand"
	"testing"
)

// TestLeopardNEONKernels checks the GF(2^16) NEON kernels against the
// reference Go implementations for random log_m values and sizes.
func TestLeopardNEONKernels(t *testing.T) {
	initConstants()
	if multiply256LUT == nil {
		t.Skip("multiply256LUT not initialized")
	}
	rng := rand.New(rand.NewSource(1))
	for iter := 0; iter < 2000; iter++ {
		logM := ffe(rng.Intn(modulus))
		if iter < 4 {
			logM = ffe(iter)
		}
		n := 64 * (1 + rng.Intn(64))
		x := make([]byte, n)
		y := make([]byte, n)
		rng.Read(x)
		rng.Read(y)
		tbl := &multiply256LUT[logM]

		// mulgf16: x = y*m
		gotMul := make([]byte, n)
		wantMul := make([]byte, n)
		mulgf16NEON(gotMul, y, tbl)
		refMul(wantMul, y, logM)
		if !bytes.Equal(gotMul, wantMul) {
			t.Fatalf("mulgf16NEON mismatch log_m=%d n=%d", logM, n)
		}

		// mulgf16Xor: x ^= y*m
		gotXor := bytes.Clone(x)
		wantXor := bytes.Clone(x)
		mulgf16XorNEON(gotXor, y, tbl)
		refMulAdd(wantXor, y, logM)
		if !bytes.Equal(gotXor, wantXor) {
			t.Fatalf("mulgf16XorNEON mismatch log_m=%d n=%d", logM, n)
		}

		// ifftDIT2: y ^= x; x ^= y*m
		gx, gy := bytes.Clone(x), bytes.Clone(y)
		wx, wy := bytes.Clone(x), bytes.Clone(y)
		ifftDIT2NEON(gx, gy, tbl)
		sliceXorGo(wx, wy, nil)
		refMulAdd(wx, wy, logM)
		if !bytes.Equal(gx, wx) || !bytes.Equal(gy, wy) {
			t.Fatalf("ifftDIT2NEON mismatch log_m=%d n=%d", logM, n)
		}

		// fftDIT2: x ^= y*m; y ^= x
		gx, gy = bytes.Clone(x), bytes.Clone(y)
		wx, wy = bytes.Clone(x), bytes.Clone(y)
		fftDIT2NEON(gx, gy, tbl)
		refMulAdd(wx, wy, logM)
		sliceXorGo(wx, wy, nil)
		if !bytes.Equal(gx, wx) || !bytes.Equal(gy, wy) {
			t.Fatalf("fftDIT2NEON mismatch log_m=%d n=%d", logM, n)
		}
	}
}

func TestLeopardNEONKernelBoundaries(t *testing.T) {
	initConstants()
	if !defaultOptions.useNEON {
		t.Skip("NEON not available")
	}
	rng := rand.New(rand.NewSource(8))
	for _, n := range []int{0, 64, 192, 4096, 25664} {
		for _, offset := range []int{0, 1, 15, 31, 63} {
			for _, logM := range []ffe{0, 1, 2, modulus - 1} {
				for kind := range 4 {
					xbuf, ybuf := make([]byte, n+128), make([]byte, n+128)
					rng.Read(xbuf)
					rng.Read(ybuf)
					xbefore, ybefore := bytes.Clone(xbuf), bytes.Clone(ybuf)
					x, y := xbuf[offset:offset+n], ybuf[offset:offset+n]
					wx, wy := bytes.Clone(x), bytes.Clone(y)
					table := &multiply256LUT[logM]
					switch kind {
					case 0:
						mulgf16NEON(x, y, table)
						refMul(wx, wy, logM)
					case 1:
						mulgf16XorNEON(x, y, table)
						refMulAdd(wx, wy, logM)
					case 2:
						fftDIT2NEON(x, y, table)
						refMulAdd(wx, wy, logM)
						sliceXorGo(wx, wy, nil)
					case 3:
						ifftDIT2NEON(x, y, table)
						sliceXorGo(wx, wy, nil)
						refMulAdd(wx, wy, logM)
					}
					if !bytes.Equal(x, wx) || !bytes.Equal(y, wy) ||
						!bytes.Equal(xbuf[:offset], xbefore[:offset]) ||
						!bytes.Equal(xbuf[offset+n:], xbefore[offset+n:]) ||
						!bytes.Equal(ybuf[:offset], ybefore[:offset]) ||
						!bytes.Equal(ybuf[offset+n:], ybefore[offset+n:]) {
						t.Fatalf("boundary mismatch: kind=%d n=%d offset=%d log=%d", kind, n, offset, logM)
					}
				}
			}
		}
	}
}

// TestLeopardNEONMulXor8 checks the 8-way accumulate wrapper against the
// reference, including zero scalars.
func TestLeopardNEONMulXor8(t *testing.T) {
	initConstants()
	if multiply256LUT == nil {
		t.Skip("multiply256LUT not initialized")
	}
	rng := rand.New(rand.NewSource(2))
	o := defaultOptions
	for iter := 0; iter < 200; iter++ {
		n := 64 * (1 + rng.Intn(32))
		in := make([]byte, n)
		rng.Read(in)
		var scalars [8]uint16
		var got, want [8][]byte
		for k := range scalars {
			if rng.Intn(4) != 0 {
				scalars[k] = uint16(rng.Intn(1 << 16))
			}
			got[k] = make([]byte, n)
			rng.Read(got[k])
			want[k] = bytes.Clone(got[k])
		}
		mulgf16Xor8(&scalars, in, &got, &o)
		refMulAdd8x(&scalars, in, &want)
		for k := range got {
			if !bytes.Equal(got[k], want[k]) {
				t.Fatalf("mulgf16Xor8 mismatch k=%d scalar=%d", k, scalars[k])
			}
		}
	}
}

// TestLeopardNEONEncodeMatchesReference encodes and reconstructs with the
// NEON path and with NEON disabled (pure-Go reference) and requires
// byte-identical shards. Shapes include the fibre default 4096+12288.
func TestLeopardNEONEncodeMatchesReference(t *testing.T) {
	if !defaultOptions.useNEON {
		t.Skip("NEON not available")
	}
	shapes := []struct{ data, parity, size int }{
		{4, 12, 64},
		{64, 192, 1024},
		{300, 700, 320},
		{4096, 12288, 256},
		{4096, 12288, 25664},
	}
	for _, sh := range shapes {
		neon, err := New(sh.data, sh.parity, WithLeopardGF16(true))
		if err != nil {
			t.Fatal(err)
		}
		ref, err := New(sh.data, sh.parity, WithLeopardGF16(true), WithNEON(false))
		if err != nil {
			t.Fatal(err)
		}
		rng := rand.New(rand.NewSource(int64(sh.data)))
		a := make([][]byte, sh.data+sh.parity)
		b := make([][]byte, sh.data+sh.parity)
		for i := range a {
			a[i] = make([]byte, sh.size)
			if i < sh.data {
				rng.Read(a[i])
			}
			b[i] = bytes.Clone(a[i])
		}
		if err := neon.Encode(a); err != nil {
			t.Fatal(err)
		}
		if err := ref.Encode(b); err != nil {
			t.Fatal(err)
		}
		for i := range a {
			if !bytes.Equal(a[i], b[i]) {
				t.Fatalf("shape %+v: shard %d differs between NEON and reference encode", sh, i)
			}
		}
		// Drop every other data shard and a slice of parity; reconstruct both ways.
		for i := 0; i < sh.data; i += 2 {
			a[i], b[i] = nil, nil
		}
		for i := sh.data; i < sh.data+sh.parity/3; i++ {
			a[i], b[i] = nil, nil
		}
		if err := neon.Reconstruct(a); err != nil {
			t.Fatal(err)
		}
		if err := ref.Reconstruct(b); err != nil {
			t.Fatal(err)
		}
		for i := range a {
			if !bytes.Equal(a[i], b[i]) {
				t.Fatalf("shape %+v: shard %d differs between NEON and reference reconstruct", sh, i)
			}
		}
	}
}

func BenchmarkLeopardGF16Encode4096x12288(b *testing.B) {
	for _, neon := range []bool{false, true} {
		name := "ref"
		if neon {
			name = "neon"
		}
		b.Run(name, func(b *testing.B) {
			enc, err := New(4096, 12288, WithLeopardGF16(true), WithNEON(neon))
			if err != nil {
				b.Fatal(err)
			}
			const size = 25600
			shards := make([][]byte, 16384)
			for i := range shards {
				shards[i] = make([]byte, size)
			}
			rand.New(rand.NewSource(1)).Read(shards[0])
			b.SetBytes(int64(4096 * size))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := enc.Encode(shards); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// Leopard GF16 encode runs on one goroutine regardless of the goroutine
// options; this benchmark documents that they have no effect on its speed.
func BenchmarkLeopardGF16EncodeGoroutineOpts(b *testing.B) {
	for _, opt := range []struct {
		name string
		opts []Option
	}{
		{"default", nil},
		{"maxGoroutines16", []Option{WithMaxGoroutines(16)}},
		{"autoGoroutines25600", []Option{WithAutoGoroutines(25600)}},
	} {
		b.Run(opt.name, func(b *testing.B) {
			enc, err := New(4096, 12288, append([]Option{WithLeopardGF16(true)}, opt.opts...)...)
			if err != nil {
				b.Fatal(err)
			}
			const size = 25600
			shards := make([][]byte, 16384)
			for i := range shards {
				shards[i] = make([]byte, size)
			}
			rand.New(rand.NewSource(1)).Read(shards[0])
			b.SetBytes(int64(4096 * size))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := enc.Encode(shards); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkLeopardNEONMulXor8(b *testing.B) {
	initConstants()
	o := defaultOptions
	scalars := [8]uint16{1, 2, 3, 0xffff, 0x1234, 0xabcd, 0x5555, 0xaaaa}
	for _, n := range []int{64, 1024, 16384, 65536, 1048576} {
		in := make([]byte, n)
		var outs [8][]byte
		for k := range outs {
			outs[k] = make([]byte, n)
		}
		for _, fused := range []bool{false, true} {
			name := "loop"
			if fused {
				name = "dispatch"
			}
			b.Run(fmt.Sprintf("%d/%s", n, name), func(b *testing.B) {
				b.SetBytes(int64(n))
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					if fused {
						mulgf16Xor8(&scalars, in, &outs, &o)
					} else {
						for k, c := range scalars {
							if c != 0 {
								mulgf16Xor(outs[k], in, logLUT[ffe(c)], &o)
							}
						}
					}
				}
			})
		}
	}
}

func TestLeopardNEONMulXor8Boundaries(t *testing.T) {
	initConstants()
	rng := rand.New(rand.NewSource(39))
	for _, n := range []int{0, 64, 128, 960, 1024, 1088, 16384} {
		for _, offset := range []int{0, 1, 15, 63} {
			for _, scalars := range [][8]uint16{{}, {1, 2, 3, 4, 0xffff, 0x1234, 0xabcd, 0x5555}, {0, 0, 1, 0, 0, 0xffff, 0, 0}} {
				inbuf := make([]byte, n+128)
				rng.Read(inbuf)
				before := bytes.Clone(inbuf)
				in := inbuf[offset : offset+n]
				var got, want, backing, initial [8][]byte
				var tables [8]*[128]uint8
				for k, c := range scalars {
					backing[k] = make([]byte, n+128)
					rng.Read(backing[k])
					initial[k] = bytes.Clone(backing[k])
					got[k] = backing[k][offset : offset+n]
					want[k] = bytes.Clone(got[k])
					if c != 0 {
						tables[k] = &multiply256LUT[logLUT[ffe(c)]]
					}
				}
				mulgf16Xor8NEON(in, &got, &tables)
				refMulAdd8x(&scalars, in, &want)
				if !bytes.Equal(inbuf, before) {
					t.Fatal("input modified")
				}
				for k := range got {
					if !bytes.Equal(got[k], want[k]) || !bytes.Equal(backing[k][:offset], initial[k][:offset]) || !bytes.Equal(backing[k][offset+n:], initial[k][offset+n:]) {
						t.Fatalf("n=%d offset=%d output=%d scalars=%v", n, offset, k, scalars)
					}
				}
			}
		}
	}
}

func TestLeopardNEONMulXor8Aliases(t *testing.T) {
	initConstants()
	o := defaultOptions
	scalars := [8]uint16{1, 2, 3, 4, 5, 6, 7, 8}
	for _, n := range []int{64, 1024, 1088} {
		for _, offset := range []int{0, 1, 32, 64} {
			a := make([]byte, 10*n+64)
			rand.New(rand.NewSource(41)).Read(a)
			b := bytes.Clone(a)
			var got, want [8][]byte
			for k := range got {
				got[k], want[k] = a[(k+1)*n:(k+2)*n], b[(k+1)*n:(k+2)*n]
			}
			got[0], want[0] = a[offset:offset+n], b[offset:offset+n]
			got[2], want[2] = a[3*n+offset:4*n+offset], b[3*n+offset:4*n+offset]
			mulgf16Xor8(&scalars, a[:n], &got, &o)
			for k, c := range scalars {
				mulgf16Xor(want[k], b[:n], logLUT[ffe(c)], &o)
			}
			if !bytes.Equal(a, b) {
				t.Fatalf("alias order changed: n=%d offset=%d", n, offset)
			}
		}
	}
}
