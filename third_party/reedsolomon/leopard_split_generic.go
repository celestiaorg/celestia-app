//go:build !arm64 || noasm || appengine || gccgo || nopshufb

package reedsolomon

// splitMulXor sets y = x, then x ^= x*log_m.
func splitMulXor(x, y []byte, log_m ffe, o *options) {
	copy(y, x)
	if log_m != modulus {
		mulgf16Xor(x, y, log_m, o)
	}
}

// xorSlices sets dst ^= srcs[0] ^ ... ^ srcs[k-1].
func xorSlices(dst []byte, srcs [][]byte, o *options) {
	xorSlicesGo(dst, srcs, o)
}
