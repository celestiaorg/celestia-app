package field

const (
	LeopardSymbolsPerChunk = 32
	LeopardChunkSize       = 2 * LeopardSymbolsPerChunk
)

// GF16FromLeopard extracts the r-th GF(2^16) element from Leopard-formatted
// bytes. Each chunk stores low bytes followed by high bytes.
func GF16FromLeopard(slab []byte, r int) uint16 {
	rb, rr := r/LeopardSymbolsPerChunk, r%LeopardSymbolsPerChunk
	off := rb * LeopardChunkSize
	return uint16(slab[off+LeopardSymbolsPerChunk+rr])<<8 | uint16(slab[off+rr])
}

// GF128ToLeopard writes g into one zero-padded Leopard chunk dst.
func GF128ToLeopard(g GF128, dst []byte) {
	_ = dst[LeopardChunkSize-1]
	clear(dst)
	for i := range GF128Width {
		dst[i] = byte(g[i] & 0xFF)
		dst[LeopardSymbolsPerChunk+i] = byte(g[i] >> 8)
	}
}

// GF128FromLeopard reads the first GF128Width GF(2^16) symbols from one
// Leopard chunk into a GF128.
func GF128FromLeopard(src []byte) GF128 {
	_ = src[LeopardChunkSize-1]
	var g GF128
	for i := range GF128Width {
		g[i] = uint16(src[LeopardSymbolsPerChunk+i])<<8 | uint16(src[i])
	}
	return g
}
