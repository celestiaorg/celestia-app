package rlc

// transposeTileGeneric scatters a 32-row Leopard tile into symbol columns.
func transposeTileGeneric(dst []byte, stride int, tile *[symbolsPerChunk * chunkSize]byte) {
	for j := range symbolsPerChunk {
		window := dst[j*stride : j*stride+chunkSize]
		for r := range symbolsPerChunk {
			window[r] = tile[r*chunkSize+j]
			window[symbolsPerChunk+r] = tile[r*chunkSize+symbolsPerChunk+j]
		}
	}
}
