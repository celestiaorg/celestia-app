package reedsolomon

import "math/bits"

// fuseTopStages reports whether the encoder can merge the IFFT's last stage
// with the FFT's first. That holds when m is a power of 4 equal to 4*dataShards:
// the IFFT's top stage then sees three zero rows per butterfly, so both
// stages together scale each of the m/4 IFFT rows by four constants.
func fuseTopStages(m, dataShards int) bool {
	return m >= 4 && m == 4*dataShards && bits.TrailingZeros(uint(m))%2 == 0
}

// topScales returns the factors c such that, after the IFFT's top stage on
// rows (x, 0, 0, 0) and the FFT's top stage, row q*m/4+i equals c[q]*x[i].
// It follows the butterflies of ifftDITEncoderGrouped and fftDIT4Ref.
func topScales(m int, ifftSkew []ffe) [4]ffe {
	mul := func(x, logM ffe) ffe {
		if logM == modulus {
			return 0
		}
		return mulLog(x, logM)
	}
	d := m / 4
	// IFFT top stage: y = x; x ^= x*m for (w0,w1,m01), (w0,w2,m02), (w1,w3,m02).
	w := [4]ffe{1, 0, 0, 0}
	im01, im02 := ifftSkew[d], ifftSkew[2*d]
	w[1] = w[0]
	w[0] ^= mul(w[0], im01)
	w[2] = w[0]
	w[0] ^= mul(w[0], im02)
	w[3] = w[1]
	w[1] ^= mul(w[1], im02)
	// FFT top stage: x ^= y*m; y ^= x.
	fft2 := func(x, y *ffe, logM ffe) {
		*x ^= mul(*y, logM)
		*y ^= *x
	}
	m01, m02, m23 := fftSkew[d-1], fftSkew[2*d-1], fftSkew[3*d-1]
	fft2(&w[0], &w[2], m02)
	fft2(&w[1], &w[3], m02)
	fft2(&w[0], &w[1], m01)
	fft2(&w[2], &w[3], m23)
	return w
}

// scaleTopQuarters fills the first quarters quarters of v from its first
// quarter x: quarter q becomes c[q]*x. v[:len(v)/4] is overwritten last.
func scaleTopQuarters(v [][]byte, quarters int, c *[4]ffe, o *options) {
	n := len(v) / 4
	for q := quarters - 1; q >= 0; q-- {
		for t := range n {
			scaleRow(v[q*n+t], v[t], c[q], o)
		}
	}
}

// scaleRow sets dst = c*src. dst may equal src.
func scaleRow(dst, src []byte, c ffe, o *options) {
	switch c {
	case 0:
		clear(dst)
	case 1:
		copy(dst, src)
	default:
		mulgf16(dst, src, logLUT[c], o)
	}
}
