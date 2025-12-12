package validator

import (
	"fmt"
	"math/rand/v2"

	"github.com/celestiaorg/rsema1d"
	"github.com/cometbft/cometbft/crypto"
	core "github.com/cometbft/cometbft/types"
)

// Set wraps [*core.ValidatorSet] with the height at which it is valid.
type Set struct {
	*core.ValidatorSet
	Height uint64
}

// GetByAddress finds a validator in the set by address.
// Returns the validator pointer from ValidatorSet.Validators (required for shard map lookups)
// and true if found, or nil and false if not found.
func (s Set) GetByAddress(address crypto.Address) (*core.Validator, bool) {
	for _, val := range s.Validators {
		if val.Address.String() == address.String() {
			return val, true
		}
	}
	return nil, false
}

// ShardMap maps validators to the row indices they are assigned.
type ShardMap map[*core.Validator][]int

// Assign returns a ShardMap containing all validators and their assigned row indices
// for the given commitment and total number of rows.
//
// It uses a chacha8 RNG with the commitment as the seed to shuffle the row indices
// using the Fisher-Yates algorithm.
func (s Set) Assign(commitment rsema1d.Commitment, totalRows int) ShardMap {
	if len(s.Validators) == 0 || totalRows == 0 {
		return make(ShardMap)
	}

	var seed [32]byte
	copy(seed[:], commitment[:])

	// chacha8 RNG with seed being the commitment
	rng := rand.New(rand.NewChaCha8(seed))

	// shuffle row indices with Fisher-Yates algorithm
	rowsIndicies := make([]int, totalRows)
	for i := range totalRows {
		rowsIndicies[i] = i
	}
	rng.Shuffle(totalRows, func(i, j int) {
		rowsIndicies[i], rowsIndicies[j] = rowsIndicies[j], rowsIndicies[i]
	})

	// assign rows to validators in a ShardMap
	shardMap := make(ShardMap)
	offset := 0
	for i, validator := range s.Validators {
		// TODO(@Wondertan): As per Nashqueue, we no longer want to send every row to validators and some might be not assigned.
		// So the number of rows to assign should be given as parameter to Assign and which probably would be taken from BlobConfig.
		rowsToAssign := totalRows / len(s.Validators)
		if i < totalRows%len(s.Validators) {
			rowsToAssign++
		}

		shardMap[validator] = rowsIndicies[offset : offset+rowsToAssign]
		offset += rowsToAssign
	}

	return shardMap
}

// Verify checks if all given row indices are assigned to [core.Validator].
// Returns error if validator is not in the map, count doesn't match, or any row is not assigned.
// This method builds a temporary set for O(r + n) complexity instead of O(n × r).
func (sm ShardMap) Verify(val *core.Validator, rowIndices []uint32) error {
	rows, ok := sm[val]
	if !ok {
		return fmt.Errorf("validator not in shard map")
	}

	// verify count matches total assigned
	if len(rowIndices) != len(rows) {
		return fmt.Errorf("expected %d rows, got %d", len(rows), len(rowIndices))
	}

	assignedSet := make(map[int]struct{}, len(rows))
	for _, idx := range rows {
		assignedSet[idx] = struct{}{}
	}

	for _, rowIdx := range rowIndices {
		if _, ok := assignedSet[int(rowIdx)]; !ok {
			return fmt.Errorf("row %d not assigned to validator", rowIdx)
		}
	}
	return nil
}
