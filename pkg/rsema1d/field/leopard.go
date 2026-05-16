package field

const (
	LeopardSymbolsPerChunk = 32
	LeopardChunkSize       = 2 * LeopardSymbolsPerChunk
)

// GF16FromLeopard extracts the r-th GF16 element from Leopard-formatted bytes.
// Each chunk stores low bytes followed by high bytes.
func GF16FromLeopard(slab []byte, r int) GF16 {
	rb, rr := r/LeopardSymbolsPerChunk, r%LeopardSymbolsPerChunk
	off := rb * LeopardChunkSize
	return GF16(uint16(slab[off+LeopardSymbolsPerChunk+rr])<<8 | uint16(slab[off+rr]))
}

// EncodeLeopardGF128 writes a GF128 into one zero-padded Leopard chunk.
func EncodeLeopardGF128(dst []byte, g GF128) {
	_ = dst[LeopardChunkSize-1]
	clear(dst)
	for i := range GF128Width {
		dst[i] = byte(g[i] & 0xFF)
		dst[LeopardSymbolsPerChunk+i] = byte(g[i] >> 8)
	}
}

// DecodeLeopardGF128 reads the first 8 GF16 symbols from one Leopard chunk.
func DecodeLeopardGF128(src []byte) GF128 {
	_ = src[LeopardChunkSize-1]
	var g GF128
	for i := range GF128Width {
		g[i] = GF16(src[LeopardSymbolsPerChunk+i])<<8 | GF16(src[i])
	}
	return g
}

// LeopardGF128BufSize returns the byte length for k GF128 values laid out as
// GF128Width concatenated Leopard-formatted component regions.
func LeopardGF128BufSize(k int) int { return GF128Width * 2 * k }

// LeopardGF128Views partitions a GF128Width-by-k Leopard buffer into component
// views suitable as MulSliceXor8 destinations.
func LeopardGF128Views(buf []byte, k int) [GF128Width][]byte {
	stride := 2 * k
	var views [GF128Width][]byte
	for i := range views {
		views[i] = buf[i*stride : (i+1)*stride]
	}
	return views
}

// GF128sFromLeopard reads a GF128Width-by-k Leopard buffer into one GF128 per row.
func GF128sFromLeopard(buf []byte, k int) []GF128 {
	out := make([]GF128, k)
	stride := 2 * k
	for comp := range GF128Width {
		view := buf[comp*stride : (comp+1)*stride]
		for r := range k {
			out[r][comp] = GF16FromLeopard(view, r)
		}
	}
	return out
}

// MulSliceXor8 applies the 8 GF16 components of coeff to one Leopard-formatted
// input slice, XOR-accumulating each component into its output view.
func MulSliceXor8(coeff *GF128, in []byte, outs *[8][]byte) {
	var s [8]uint16
	for k, v := range coeff {
		s[k] = uint16(v)
	}
	ll.GF16MulSliceXor8(&s, in, outs)
}
