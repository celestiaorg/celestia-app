//go:build !noasm && !appengine && !gccgo && !nopshufb

// Copyright 2015, Klaus Post, see LICENSE for details.
// Copyright 2017, Minio, Inc.

package reedsolomon

import "unsafe"

const pshufb = true

//go:noescape
func galMulNEON(low, high, in, out []byte)

//go:noescape
func galMulXorNEON(low, high, in, out []byte)

//go:noescape
func ifftDIT2NEON(x, y []byte, table *[128]uint8)

//go:noescape
func fftDIT2NEON(x, y []byte, table *[128]uint8)

//go:noescape
func mulgf16NEON(x, y []byte, table *[128]uint8)

//go:noescape
func mulgf16XorNEON(x, y []byte, table *[128]uint8)

//go:noescape
func mulgf16Xor8NEON(in []byte, outs *[8][]byte, tables *[8]*[128]uint8)

//go:noescape
func splitMulXorNEON(x, y []byte, table *[128]uint8)

//go:noescape
func ifftDIT2NEON3(x, y []byte, table *[128]uint8)

//go:noescape
func fftDIT2NEON3(x, y []byte, table *[128]uint8)

//go:noescape
func mulgf16XorNEON3(x, y []byte, table *[128]uint8)

//go:noescape
func splitMulXorNEON3(x, y []byte, table *[128]uint8)

// leopardNEON reports whether the GF(2^16) NEON kernels can be used. They
// consume whole 64-byte blocks; callers pass the remainder to the reference
// code, which handles the same block granularity.
func leopardNEON(o *options) bool {
	return o.useNEON && multiply256LUT != nil
}

func getVectorLength() (vl, pl uint64)

func init() {
	if defaultOptions.useSVE {
		if vl, _ := getVectorLength(); vl <= 256 {
			// set vector length in bytes
			defaultOptions.vectorLength = int(vl) >> 3
		} else {
			// disable SVE for hardware implementatons over 256 bits (only know to be Fujitsu A64FX atm)
			defaultOptions.useSVE = false
		}
	}
}

func galMulSlice(c byte, in, out []byte, o *options) {
	// The kernels below write len(in) bytes to out without consulting its
	// length. Fail here rather than past the end of out.
	out = out[:len(in)]
	if c == 1 {
		copy(out, in)
		return
	}
	var done int
	done = (len(in) >> 5) << 5
	if raceEnabled {
		raceReadSlice(in[:done])
		raceWriteSlice(out[:done])
	}
	galMulNEON(mulTableLow[c][:], mulTableHigh[c][:], in, out)

	remain := len(in) - done
	if remain > 0 {
		mt := mulTable[c][:256]
		for i := done; i < len(in); i++ {
			out[i] = mt[in[i]]
		}
	}
}

func galMulSliceXor(c byte, in, out []byte, o *options) {
	out = out[:len(in)]
	if c == 1 {
		sliceXor(in, out, o)
		return
	}
	done := (len(in) >> 5) << 5
	if raceEnabled {
		raceReadSlice(in[:done])
		raceWriteSlice(out[:done])
	}
	galMulXorNEON(mulTableLow[c][:], mulTableHigh[c][:], in, out)

	remain := len(in) - done
	if remain > 0 {
		mt := mulTable[c][:256]
		for i := done; i < len(in); i++ {
			out[i] ^= mt[in[i]]
		}
	}
}

// dit4BlockSize splits 4-way butterflies into byte ranges small enough for
// the four rows to stay in L1 between the two butterfly layers.
const dit4BlockSize = 1024

// dit4Block returns the four rows of a butterfly restricted to [off, end).
func dit4Block(work [][]byte, dist, off, end int) [4][]byte {
	return [4][]byte{work[0][off:end], work[dist][off:end], work[2*dist][off:end], work[3*dist][off:end]}
}

// 4-way butterfly
func ifftDIT4(work [][]byte, dist int, log_m01, log_m23, log_m02 ffe, o *options) {
	if n := len(work[0]); leopardNEON(o) && n > dit4BlockSize {
		for off := 0; off < n; off += dit4BlockSize {
			w := dit4Block(work, dist, off, min(off+dit4BlockSize, n))
			ifftDIT4Ref(w[:], 1, log_m01, log_m23, log_m02, o)
		}
		return
	}
	ifftDIT4Ref(work, dist, log_m01, log_m23, log_m02, o)
}

// 4-way butterfly
func ifftDIT48(work [][]byte, dist int, log_m01, log_m23, log_m02 ffe8, o *options) {
	ifftDIT4Ref8(work, dist, log_m01, log_m23, log_m02, o)
}

// 4-way butterfly
func fftDIT4(work [][]byte, dist int, log_m01, log_m23, log_m02 ffe, o *options) {
	if n := len(work[0]); leopardNEON(o) && n > dit4BlockSize {
		for off := 0; off < n; off += dit4BlockSize {
			w := dit4Block(work, dist, off, min(off+dit4BlockSize, n))
			fftDIT4Ref(w[:], 1, log_m01, log_m23, log_m02, o)
		}
		return
	}
	fftDIT4Ref(work, dist, log_m01, log_m23, log_m02, o)
}

// 4-way butterfly
func fftDIT48(work [][]byte, dist int, log_m01, log_m23, log_m02 ffe8, o *options) {
	fftDIT4Ref8(work, dist, log_m01, log_m23, log_m02, o)
}

// 2-way butterfly forward
func fftDIT2(x, y []byte, log_m ffe, o *options) {
	if leopardNEON(o) {
		done := len(x) &^ 63
		if raceEnabled {
			raceWriteSlice(x[:done])
			raceWriteSlice(y[:done])
		}
		if o.useSHA3 {
			fftDIT2NEON3(x[:done], y[:done], &multiply256LUT[log_m])
		} else {
			fftDIT2NEON(x[:done], y[:done], &multiply256LUT[log_m])
		}
		if done == len(x) {
			return
		}
		x, y = x[done:], y[done:]
	}
	// Reference version:
	refMulAdd(x, y, log_m)
	// 64 byte aligned, always full.
	xorSliceNEON(x, y)
}

// 2-way butterfly forward
func fftDIT28(x, y []byte, log_m ffe8, o *options) {
	// Reference version:
	mulAdd8(x, y, log_m, o)
	sliceXor(x, y, o)
}

// 2-way butterfly
func ifftDIT2(x, y []byte, log_m ffe, o *options) {
	if leopardNEON(o) {
		done := len(x) &^ 63
		if raceEnabled {
			raceWriteSlice(x[:done])
			raceWriteSlice(y[:done])
		}
		if o.useSHA3 {
			ifftDIT2NEON3(x[:done], y[:done], &multiply256LUT[log_m])
		} else {
			ifftDIT2NEON(x[:done], y[:done], &multiply256LUT[log_m])
		}
		if done == len(x) {
			return
		}
		x, y = x[done:], y[done:]
	}
	// 64 byte aligned, always full.
	xorSliceNEON(x, y)
	// Reference version:
	refMulAdd(x, y, log_m)
}

// 2-way butterfly inverse
func ifftDIT28(x, y []byte, log_m ffe8, o *options) {
	// Reference version:
	sliceXor(x, y, o)
	mulAdd8(x, y, log_m, o)
}

func mulgf16(x, y []byte, log_m ffe, o *options) {
	if leopardNEON(o) {
		done := len(x) &^ 63
		if raceEnabled {
			raceReadSlice(y[:done])
			raceWriteSlice(x[:done])
		}
		mulgf16NEON(x[:done], y[:done], &multiply256LUT[log_m])
		if done == len(x) {
			return
		}
		x, y = x[done:], y[done:]
	}
	refMul(x, y, log_m)
}

func mulgf16Xor8(scalars *[8]uint16, in []byte, outs *[8][]byte, o *options) {
	if leopardNEON(o) {
		// Larger buffers benefit from keeping each scalar's tables in registers.
		// Preserve the original ordered operations for overlapping buffers.
		if len(in) > 1024 || mulgf16Xor8Overlap(in, outs) {
			for k, c := range scalars {
				if c != 0 {
					mulgf16Xor(outs[k], in, logLUT[ffe(c)], o)
				}
			}
			return
		}
		var tables [8]*[128]uint8
		for k, c := range scalars {
			if c != 0 {
				tables[k] = &multiply256LUT[logLUT[ffe(c)]]
				if raceEnabled {
					raceWriteSlice(outs[k])
				}
			}
		}
		if raceEnabled {
			raceReadSlice(in)
		}
		mulgf16Xor8NEON(in, outs, &tables)
		return
	}
	refMulAdd8x(scalars, in, outs)
}

func mulgf16Xor(x, y []byte, log_m ffe, o *options) {
	if leopardNEON(o) {
		done := len(x) &^ 63
		if raceEnabled {
			raceReadSlice(y[:done])
			raceWriteSlice(x[:done])
		}
		if o.useSHA3 {
			mulgf16XorNEON3(x[:done], y[:done], &multiply256LUT[log_m])
		} else {
			mulgf16XorNEON(x[:done], y[:done], &multiply256LUT[log_m])
		}
		if done == len(x) {
			return
		}
		x, y = x[done:], y[done:]
	}
	refMulAdd(x, y, log_m)
}

// splitMulXor sets y = x, then x ^= x*log_m.
func splitMulXor(x, y []byte, log_m ffe, o *options) {
	if log_m == modulus {
		copy(y, x)
		return
	}
	if leopardNEON(o) {
		done := len(x) &^ 63
		if raceEnabled {
			raceWriteSlice(x[:done])
			raceWriteSlice(y[:done])
		}
		if o.useSHA3 {
			splitMulXorNEON3(x[:done], y[:done], &multiply256LUT[log_m])
		} else {
			splitMulXorNEON(x[:done], y[:done], &multiply256LUT[log_m])
		}
		if done == len(x) {
			return
		}
		x, y = x[done:], y[done:]
	}
	copy(y, x)
	refMulAdd(x, y, log_m)
}

func mulAdd8(out, in []byte, log_m ffe8, o *options) {
	t := &multiply256LUT8[log_m]
	galMulXorNEON(t[:16], t[16:32], in, out)
	done := (len(in) >> 5) << 5
	in = in[done:]
	if len(in) > 0 {
		out = out[done:]
		refMulAdd8(out, in, log_m)
	}
}

func mulgf8(out, in []byte, log_m ffe8, o *options) {
	var done int
	t := &multiply256LUT8[log_m]
	galMulNEON(t[:16], t[16:32], in, out)
	done = (len(in) >> 5) << 5

	remain := len(in) - done
	if remain > 0 {
		mt := mul8LUTs[log_m].Value[:]
		for i := done; i < len(in); i++ {
			out[i] = byte(mt[in[i]])
		}
	}
}

// 4-way butterfly with separate destination
func ifftDIT4Dst(dst, work [][]byte, dist int, log_m01, log_m23, log_m02 ffe, o *options) {
	ifftDIT4DstRef(dst, work, dist, log_m01, log_m23, log_m02, o)
}

// 4-way butterfly with separate destination
func ifftDIT48Dst(dst, work [][]byte, dist int, log_m01, log_m23, log_m02 ffe8, o *options) {
	// Fall back. Should not be called.
	ifftDIT4DstRef8(dst, work, dist, log_m01, log_m23, log_m02, o)
}

// mulgf16Xor8Overlap detects aliases whose update order must be preserved.
func mulgf16Xor8Overlap(in []byte, outs *[8][]byte) bool {
	if len(in) == 0 {
		return false
	}
	var starts [9]uintptr
	starts[0] = uintptr(unsafe.Pointer(unsafe.SliceData(in)))
	for k := range outs {
		start := uintptr(unsafe.Pointer(unsafe.SliceData(outs[k])))
		for _, prev := range starts[:k+1] {
			if start < prev+uintptr(len(in)) && prev < start+uintptr(len(in)) {
				return true
			}
		}
		starts[k+1] = start
	}
	return false
}
