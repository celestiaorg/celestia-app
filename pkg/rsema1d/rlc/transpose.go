package rlc

// transposeTile scatters a 32-row Leopard tile into symbol columns. It is the
// portable fallback used on every architecture that does not take the PMULL
// fast path, including arm64 CPUs without PMULL+SHA3. (A pure-Go SWAR
// replacement is tracked separately and not part of this change.)
func transposeTile(dst []byte, stride int, tile *[symbolsPerChunk * chunkSize]byte) {
	for j := range symbolsPerChunk {
		window := dst[j*stride : j*stride+chunkSize]
		for r := range symbolsPerChunk {
			window[r] = tile[r*chunkSize+j]
			window[symbolsPerChunk+r] = tile[r*chunkSize+symbolsPerChunk+j]
		}
	}
}
