//go:build !arm64 || noasm || nopshufb || race

package rlc

func transposeTile(dst []byte, stride int, tile *[symbolsPerChunk * chunkSize]byte) {
	transposeTileGeneric(dst, stride, tile)
}
