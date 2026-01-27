package validator

import (
	"fmt"
	"math/rand/v2"

	"github.com/celestiaorg/rsema1d"
	"github.com/cometbft/cometbft/crypto"
	cmtmath "github.com/cometbft/cometbft/libs/math"
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
// for the given commitment.
//
// Rows are distributed based on the relation: originalRows / rows = livenessThreshold / stake%
// This means 33% stake should have originalRows (4096), so each validator gets:
// rows = ceil(originalRows * stake% / livenessThreshold)
//
// The minRows parameter ensures every validator receives at least that many rows
// for unique decodability security, even if their proportional share would be less.
//
// When the sum of assigned rows exceeds totalRows (due to minRows floor guarantees),
// row indices wrap around using modulo arithmetic. This means the same row may be
// assigned to multiple validators, ensuring all validators receive their required
// minimum while maintaining deterministic assignment.
//
// It uses a ChaCha8 RNG seeded with the commitment to shuffle the row indices
// using the Fisher-Yates algorithm, ensuring deterministic and uniform distribution.
func (s Set) Assign(commitment rsema1d.Commitment, totalRows, originalRows, minRows int, livenessThreshold cmtmath.Fraction) ShardMap {
	if len(s.Validators) == 0 || totalRows == 0 || minRows == 0 {
		return make(ShardMap)
	}

	// rows = ceil(originalRows * stake% / livenessThreshold)
	//      = ceil(originalRows * votingPower * denominator / (totalVotingPower * numerator))
	// Capped at originalRows since that's all needed for reconstruction.
	rowsPerValidator := make([]int, len(s.Validators))
	for i, v := range s.Validators {
		num := int64(originalRows) * v.VotingPower * int64(livenessThreshold.Denominator)
		den := s.TotalVotingPower() * int64(livenessThreshold.Numerator)
		rows := int((num + den - 1) / den) // ceil division
		rowsPerValidator[i] = min(max(rows, minRows), originalRows)
	}

	var seed [32]byte
	copy(seed[:], commitment[:])

	// chacha8 RNG with seed being the commitment
	rng := rand.New(rand.NewChaCha8(seed))

	// shuffle all totalRows indices with Fisher-Yates algorithm
	// NOTE: std library Shuffle implements Fisher-Yates algorithm
	rowsIndicies := make([]int, totalRows)
	for i := range totalRows {
		rowsIndicies[i] = i
	}
	rng.Shuffle(totalRows, func(i, j int) {
		rowsIndicies[i], rowsIndicies[j] = rowsIndicies[j], rowsIndicies[i]
	})

	// assign rows to validators, wrapping around with modulo if total assigned exceeds totalRows
	shardMap := make(ShardMap)
	offset := 0
	for i, validator := range s.Validators {
		rows := make([]int, rowsPerValidator[i])
		for j := range rows {
			// modulo ensures wrap-around when minRows causes over-assignment
			rows[j] = rowsIndicies[(offset+j)%totalRows]
		}
		shardMap[validator] = rows
		offset += rowsPerValidator[i]
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
