package validator_test

import (
	"crypto/sha256"
	"testing"

	"github.com/celestiaorg/celestia-app-fibre/v6/fibre/validator"
	"github.com/celestiaorg/rsema1d"
	"github.com/cometbft/cometbft/crypto/ed25519"
	cmtmath "github.com/cometbft/cometbft/libs/math"
	core "github.com/cometbft/cometbft/types"
	"github.com/stretchr/testify/require"
)

// default liveness threshold for tests (1/3)
var testLivenessThreshold = cmtmath.Fraction{Numerator: 1, Denominator: 3}

func TestSet_Assign(t *testing.T) {
	commitment := rsema1d.Commitment{
		1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16,
		17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32,
	}

	t.Run("EmptySet", func(t *testing.T) {
		valSet := makeValidatorSet(0)

		shardMap := valSet.Assign(commitment, 100, 25, 10, testLivenessThreshold)
		require.NotNil(t, shardMap)
		require.Empty(t, shardMap)
	})

	t.Run("ZeroTotalRows", func(t *testing.T) {
		valSet := makeValidatorSet(3)

		shardMap := valSet.Assign(commitment, 0, 0, 10, testLivenessThreshold)
		require.NotNil(t, shardMap)
		require.Empty(t, shardMap)
	})

	t.Run("ZeroMinRows", func(t *testing.T) {
		valSet := makeValidatorSet(3)

		shardMap := valSet.Assign(commitment, 100, 25, 0, testLivenessThreshold)
		require.NotNil(t, shardMap)
		require.Empty(t, shardMap)
	})

	t.Run("SingleValidator", func(t *testing.T) {
		valSet := makeValidatorSet(1)

		// Single validator with 100% stake would get ceil(originalRows * 1.0 / (1/3)) = originalRows * 3 rows
		// But capped at originalRows since that's all needed for reconstruction
		totalRows := 30
		originalRows := 10
		shardMap := valSet.Assign(commitment, totalRows, originalRows, 1, testLivenessThreshold)
		require.NotNil(t, shardMap)
		require.Len(t, shardMap, 1)

		// Single validator gets capped at originalRows
		for _, rows := range shardMap {
			require.Len(t, rows, originalRows)
		}
	})

	t.Run("EqualStakesDistribution", func(t *testing.T) {
		// Test with equal stakes - all validators should get equal rows
		valSet := makeValidatorSet(100)

		hasher := sha256.New()
		hasher.Write([]byte("distribution-test"))
		var distCommitment rsema1d.Commitment
		copy(distCommitment[:], hasher.Sum(nil))

		// With 100 validators each having 1% stake and livenessThreshold=1/3:
		// rows = ceil(originalRows * 0.01 / (1/3)) = ceil(originalRows * 0.03)
		// For originalRows=1000, each gets ceil(30) = 30 rows, total = 3000
		totalRows := 3000
		originalRows := 1000
		minRows := 1 // low minRows so proportional distribution dominates
		expectedRowsPerValidator := 30
		shardMap := valSet.Assign(distCommitment, totalRows, originalRows, minRows, testLivenessThreshold)

		require.Len(t, shardMap, len(valSet.Validators), "All validators should be in shard map")

		totalAssigned := 0

		// Track all assigned rows globally to detect cross-validator duplicates
		globalSeen := make(map[int]bool)

		for val, rows := range shardMap {
			count := len(rows)
			totalAssigned += count

			// With equal stakes, each validator gets exactly expectedRowsPerValidator rows
			require.Equal(t, expectedRowsPerValidator, count, "validator %s should have exactly %d rows", val.Address, expectedRowsPerValidator)

			// Verify row indices are unique (within validator and across all validators) and in range
			for _, rowIdx := range rows {
				require.False(t, globalSeen[rowIdx], "duplicate row index %d across validators", rowIdx)
				require.GreaterOrEqual(t, rowIdx, 0)
				globalSeen[rowIdx] = true
			}
		}

		require.Equal(t, totalRows, totalAssigned, "All rows should be assigned")
		t.Logf("Total assigned: %d (rowsPerValidator=%d, validators=%d)", totalAssigned, expectedRowsPerValidator, len(valSet.Validators))
	})

	t.Run("StakeProportionalDistribution", func(t *testing.T) {
		// Create validators with different stakes: 1, 2, 3 (total = 6)
		valSet := makeValidatorSetWithStakes([]int64{1, 2, 3})

		hasher := sha256.New()
		hasher.Write([]byte("stake-proportional-test"))
		var distCommitment rsema1d.Commitment
		copy(distCommitment[:], hasher.Sum(nil))

		// With stakes 1,2,3 (total=6) and livenessThreshold=1/3:
		// rows = min(ceil(originalRows * stake * 3 / totalStake), originalRows)
		// For originalRows=18: stake1 gets min(ceil(18*1*3/6),18)=9, stake2 gets min(ceil(18*2*3/6),18)=18, stake3 gets min(ceil(18*3*3/6),18)=18
		// Use larger originalRows so cap doesn't affect smaller stakes
		totalRows := 54
		originalRows := 18
		minRows := 1 // low minRows so proportional distribution dominates
		shardMap := valSet.Assign(distCommitment, totalRows, originalRows, minRows, testLivenessThreshold)

		require.Len(t, shardMap, 3, "All validators should be in shard map")

		totalAssigned := 0
		globalSeen := make(map[int]bool)

		for val, rows := range shardMap {
			count := len(rows)
			totalAssigned += count

			// rows = min(ceil(originalRows * votingPower * 3 / totalStake), originalRows)
			formulaRows := int((int64(originalRows)*val.VotingPower*3 + 5) / 6) // ceil division
			expectedRows := min(formulaRows, originalRows)                      // cap at originalRows
			require.Equal(t, expectedRows, count, "validator with stake %d should have %d rows, got %d",
				val.VotingPower, expectedRows, count)

			for _, rowIdx := range rows {
				require.False(t, globalSeen[rowIdx], "duplicate row index %d across validators", rowIdx)
				require.GreaterOrEqual(t, rowIdx, 0)
				globalSeen[rowIdx] = true
			}
		}

		t.Logf("Total assigned: %d (totalRows=%d, stakes=1,2,3)", totalAssigned, totalRows)
	})

	t.Run("MinRowsFloor", func(t *testing.T) {
		// Create validators with different stakes: 1, 2, 3 (total = 6)
		// With small stakes, formula would give small values for low-stake validators
		// But minRows should ensure everyone gets at least minRows
		// Note: minRows must be <= originalRows
		valSet := makeValidatorSetWithStakes([]int64{1, 2, 3})

		hasher := sha256.New()
		hasher.Write([]byte("minrows-floor-test"))
		var distCommitment rsema1d.Commitment
		copy(distCommitment[:], hasher.Sum(nil))

		// With stakes 1,2,3 (total=6) and livenessThreshold=1/3:
		// Formula: rows = ceil(originalRows * stake * 3 / totalStake)
		// stake1: ceil(6*1*3/6) = 3 -> but minRows=5 floors it to 5
		// stake2: ceil(6*2*3/6) = 6 -> capped at originalRows=6
		// stake3: ceil(6*3*3/6) = 9 -> capped at originalRows=6
		// Total = 5+6+6 = 17 rows, with wrap-around since totalRows=18
		totalRows := 18
		originalRows := 6
		minRows := 5 // must be <= originalRows
		shardMap := valSet.Assign(distCommitment, totalRows, originalRows, minRows, testLivenessThreshold)

		require.Len(t, shardMap, 3, "All validators should be in shard map")

		totalAssigned := 0

		for val, rows := range shardMap {
			count := len(rows)
			totalAssigned += count

			// Every validator should get at least minRows (capped at originalRows)
			require.GreaterOrEqual(t, count, minRows, "validator with stake %d should have at least %d rows, got %d",
				val.VotingPower, minRows, count)
			require.LessOrEqual(t, count, originalRows, "validator with stake %d should have at most %d rows, got %d",
				val.VotingPower, originalRows, count)

			// All row indices should be within [0, totalRows)
			for _, rowIdx := range rows {
				require.GreaterOrEqual(t, rowIdx, 0)
				require.Less(t, rowIdx, totalRows, "row index %d should be less than totalRows %d", rowIdx, totalRows)
			}
		}

		t.Logf("Total assigned: %d (totalRows=%d, minRows=%d, originalRows=%d, stakes=1,2,3)", totalAssigned, totalRows, minRows, originalRows)
	})

	t.Run("Determinism", func(t *testing.T) {
		valSet := makeValidatorSet(3)

		totalRows := 51
		originalRows := 17
		minRows := 5
		firstRun := valSet.Assign(commitment, totalRows, originalRows, minRows, testLivenessThreshold)
		secondRun := valSet.Assign(commitment, totalRows, originalRows, minRows, testLivenessThreshold)

		require.Len(t, firstRun, len(secondRun))

		for val, rows := range firstRun {
			secondRows, ok := secondRun[val]
			require.True(t, ok, "validator %s missing in second run", val.Address)
			require.Equal(t, rows, secondRows, "row assignments differ for validator %s", val.Address)
		}
	})
}

// Results for 16,384 rows (K=4096, N=12288):
//
// CPU: AMD Ryzen 9 7940HS w/ Radeon 780M Graphics
// Results with Fisher-Yates shuffle + ChaCha8 RNG (averaged over 5 iterations):
//
//	Validators    Time/op      Memory/op    Allocs/op
//	10            ~105 µs      ~132 KB      ~7
//	50            ~106 µs      ~136 KB      ~11
//	100           ~109 µs      ~141 KB      ~13
func BenchmarkSet_Assign(b *testing.B) {
	commitment := rsema1d.Commitment{
		1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16,
		17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32,
	}

	totalRows := 16384   // typical total rows
	originalRows := 4096 // typical original rows
	minRows := 16        // typical minimum rows per validator
	livenessThreshold := cmtmath.Fraction{Numerator: 1, Denominator: 3}

	benchmarks := []struct {
		name          string
		numValidators int
	}{
		{"10_validators", 10},
		{"50_validators", 50},
		{"100_validators", 100},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			valSet := makeValidatorSet(bm.numValidators)
			for b.Loop() {
				_ = valSet.Assign(commitment, totalRows, originalRows, minRows, livenessThreshold)
			}
		})
	}
}

func TestShardMap_Verify(t *testing.T) {
	commitment := rsema1d.Commitment{
		1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16,
		17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32,
	}
	valSet := makeValidatorSet(3)
	// 3 validators with equal stake (1/3 each), livenessThreshold=1/3
	// rows = ceil(4 * (1/3) * 3) = 4 per validator, total = 12
	shardMap := valSet.Assign(commitment, 12, 4, 1, testLivenessThreshold)

	// test valid assignments
	for val, rows := range shardMap {
		rowIndices := make([]uint32, len(rows))
		for i, idx := range rows {
			rowIndices[i] = uint32(idx)
		}
		require.NoError(t, shardMap.Verify(val, rowIndices))
	}

	// test too few rows provided
	for val, rows := range shardMap {
		if len(rows) < 2 {
			continue
		}
		// provide only first row when multiple are expected
		partialRows := []uint32{uint32(rows[0])}
		require.ErrorContains(t, shardMap.Verify(val, partialRows), "expected")
		break
	}

	// test too many rows provided
	for val, rows := range shardMap {
		if len(rows) == 0 {
			continue
		}
		// provide extra row that isn't assigned
		extraRows := make([]uint32, len(rows)+1)
		for i, idx := range rows {
			extraRows[i] = uint32(idx)
		}
		extraRows[len(rows)] = 999 // extra row not assigned
		require.ErrorContains(t, shardMap.Verify(val, extraRows), "expected")
		break
	}

	// test invalid row index with correct count
	for val, rows := range shardMap {
		if len(rows) == 0 {
			continue
		}
		// replace valid row with invalid one (999 not assigned)
		invalidRows := make([]uint32, len(rows))
		for i, idx := range rows {
			invalidRows[i] = uint32(idx)
		}
		invalidRows[0] = 999 // replace first with invalid
		require.ErrorContains(t, shardMap.Verify(val, invalidRows), "not assigned")
		break
	}
}

func makeValidatorSet(n int) validator.Set {
	validators := make([]*core.Validator, n)
	for i := range n {
		privKey := ed25519.GenPrivKey()
		validators[i] = core.NewValidator(privKey.PubKey(), 1)
	}
	return validator.Set{
		ValidatorSet: core.NewValidatorSet(validators),
		Height:       100,
	}
}

func makeValidatorSetWithStakes(stakes []int64) validator.Set {
	validators := make([]*core.Validator, len(stakes))
	for i, stake := range stakes {
		privKey := ed25519.GenPrivKey()
		validators[i] = core.NewValidator(privKey.PubKey(), stake)
	}
	return validator.Set{
		ValidatorSet: core.NewValidatorSet(validators),
		Height:       100,
	}
}
