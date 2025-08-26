package rsema1d

import (
	"github.com/celestiaorg/rsema1d/field"
	"github.com/celestiaorg/rsema1d/merkle"
)

// buildPaddedRowTree creates a padded Merkle tree from extended rows
func buildPaddedRowTree(extended [][]byte, config *Config) *merkle.Tree {
	zeroRow := make([]byte, config.RowSize)
	paddedRows := make([][]byte, config.totalPadded)
	
	// Fill padded array: [original | padding | extended | padding]
	copy(paddedRows[0:config.K], extended[0:config.K])                           // Original rows
	for i := config.K; i < config.kPadded; i++ {
		paddedRows[i] = zeroRow                                                  // Padding after K
	}
	copy(paddedRows[config.kPadded:config.kPadded+config.N], extended[config.K:]) // Extended rows  
	for i := config.kPadded + config.N; i < config.totalPadded; i++ {
		paddedRows[i] = zeroRow                                                  // Padding at end
	}
	
	return merkle.NewTree(paddedRows)
}

// buildPaddedRLCTree creates a padded Merkle tree from extended RLC values
func buildPaddedRLCTree(rlcExtended []field.GF128, config *Config) *merkle.Tree {
	zeroRLC := make([]byte, 16) // Zero GF128 value
	paddedRLCLeaves := make([][]byte, config.totalPadded)
	
	// Fill matching row tree layout
	for i := 0; i < config.K; i++ {
		bytes := field.ToBytes128(rlcExtended[i])
		paddedRLCLeaves[i] = bytes[:]
	}
	for i := config.K; i < config.kPadded; i++ {
		paddedRLCLeaves[i] = zeroRLC // Padding
	}
	for i := 0; i < config.N; i++ {
		bytes := field.ToBytes128(rlcExtended[config.K+i])
		paddedRLCLeaves[config.kPadded+i] = bytes[:]
	}
	for i := config.kPadded + config.N; i < config.totalPadded; i++ {
		paddedRLCLeaves[i] = zeroRLC // Padding
	}
	
	return merkle.NewTree(paddedRLCLeaves)
}

// mapIndexToTreePosition maps an actual row index to its position in the padded tree
func mapIndexToTreePosition(index int, config *Config) int {
	if index < config.K {
		return index // Original rows stay at same position
	}
	return config.kPadded + (index - config.K) // Extended rows shifted by padding
}