package rsema1d

import (
	"github.com/celestiaorg/celestia-app/v9/pkg/rsema1d/field"
	"github.com/celestiaorg/celestia-app/v9/pkg/rsema1d/merkle"
	"github.com/celestiaorg/celestia-app/v9/pkg/rsema1d/rlc"
)

// buildPaddedRowTree creates a padded Merkle tree from extended rows.
// rowSize is derived from extended[0] rather than config.RowSize so the
// Coder's deferred-RowSize mode (config.RowSize=0, reused across blobs)
// still produces correctly-sized leaf scratch buffers.
func buildPaddedRowTree(extended [][]byte, config *Config) *merkle.Tree {
	rowSize := len(extended[0])
	return merkle.NewTreeFromWriter(config.totalPadded, rowSize, config.WorkerCount, func(i int, dst []byte) {
		switch {
		case i < config.K:
			copy(dst, extended[i])
		case i >= config.kPadded && i < config.kPadded+config.N:
			copy(dst, extended[config.K+i-config.kPadded])
		}
	})
}

// buildPaddedRLCTree creates a padded Merkle tree from RLC original values
// Only stores K values padded to kPadded (not totalPadded like row tree)
func buildPaddedRLCTree(rlcOrig rlc.Vector, config *Config) *merkle.Tree {
	return merkle.NewTreeFromWriter(config.kPadded, field.GF128Size, config.WorkerCount, func(i int, dst []byte) {
		if i < config.K {
			field.EncodeGF128(dst, rlcOrig[i])
		}
	})
}

// mapIndexToTreePosition maps an actual row index to its position in the padded tree
func mapIndexToTreePosition(index int, config *Config) int {
	if index < config.K {
		return index // Original rows stay at same position
	}
	return config.kPadded + (index - config.K) // Extended rows shifted by padding
}
