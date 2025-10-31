package validator

import (
	"fmt"
	"math/big"

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
// for the given commitment and total number of rows. The assignment uses the formula:
//
//	validator_index = (commitment + row_index) mod num_validators
//
// The commitment is converted to a big.Int, added to the row index, and the result
// is taken modulo the number of validators to determine the assignment.
//
// TODO(@Wondertan): This assignment algorithm is not final and may be changed
// to improve distribution properties or security guarantees.
func (s Set) Assign(commitment rsema1d.Commitment, totalRows int) ShardMap {
	if len(s.Validators) == 0 || totalRows == 0 {
		return make(ShardMap)
	}

	shardMap := make(ShardMap)

	// TODO(@Wondertan): If we ever end up using this assignment algorithm,
	// we should move to uint256 libraries for up to 60% speedups per arithmetic operations
	commitmentInt := new(big.Int).SetBytes(commitment[:])
	valLenBig := big.NewInt(int64(len(s.Validators)))

	for rowIndex := range totalRows {
		rowIndexInt := big.NewInt(int64(rowIndex))
		sum := new(big.Int).Add(commitmentInt, rowIndexInt)
		idx := new(big.Int).Mod(sum, valLenBig)
		validator := s.Validators[idx.Int64()]
		shardMap[validator] = append(shardMap[validator], rowIndex)
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
