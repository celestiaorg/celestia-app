//go:build arm64 && !noasm && !nopshufb && !race

package rlc

import "golang.org/x/sys/cpu"

func transposeTile(dst []byte, stride int, tile *[symbolsPerChunk * chunkSize]byte) {
	if !cpu.ARM64.HasASIMD {
		transposeTileGeneric(dst, stride, tile)
		return
	}
	// Write complete cache lines when scattering to widely spaced columns.
	// Otherwise eight-byte stores repeatedly revisit the same cache sets.
	var transposed [symbolsPerChunk * chunkSize]byte
	transposeTileNEON(&transposed, tile)
	for j := range symbolsPerChunk {
		copy(dst[j*stride:j*stride+chunkSize], transposed[j*chunkSize:(j+1)*chunkSize])
	}
}

//go:noescape
func transposeTileNEON(dst, tile *[symbolsPerChunk * chunkSize]byte)
