package validator

import (
	"math/big"

	"github.com/celestiaorg/rsema1d"
	core "github.com/cometbft/cometbft/types"
)

// Set wraps [*core.ValidatorSet] with the height at which it is valid.
type Set struct {
	*core.ValidatorSet
	Height uint64
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
