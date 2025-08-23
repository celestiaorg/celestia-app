package encoding

import (
	"errors"

	"github.com/celestiaorg/rsema1d/field"
)

// extendVertical performs vertical RS extension using Leopard (internal)
func ExtendVertical(data [][]byte, k, n int) ([][]byte, error) {
	// TODO: Implement using github.com/celestiaorg/reedsolomon
	// - Create encoder with k data shards and n parity shards
	// - Each row is a shard
	// - Use encoder.Encode() to generate parity rows
	return nil, errors.New("not implemented - waiting for reedsolomon fork")
}

// extendRLCResults extends RLC results using Reed-Solomon
func ExtendRLCResults(rlcOriginal []field.GF128, k, n int) ([]field.GF128, error) {
	// TODO: Implement RLC extension
	// - Convert GF128 values to byte shards (16 bytes each)
	// - Pad to 64 bytes minimum (Leopard requirement)
	// - Use ExtendVertical to generate parity
	// - Extract extended RLC values from parity shards
	return nil, errors.New("not implemented - waiting for reedsolomon fork")
}

// Reconstruct recovers original data from any K rows
func Reconstruct(rows [][]byte, indices []int, k int) ([][]byte, error) {
	// TODO: Implement using Leopard RS decoding
	// - Create decoder
	// - Mark missing shards
	// - Use decoder.Reconstruct() to recover
	return nil, errors.New("not implemented - waiting for reedsolomon fork")
}