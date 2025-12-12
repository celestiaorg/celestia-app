package validator_test

import (
	"crypto/sha256"
	"testing"

	"github.com/celestiaorg/celestia-app/v6/fibre/validator"
	"github.com/celestiaorg/rsema1d"
	"github.com/cometbft/cometbft/crypto/ed25519"
	core "github.com/cometbft/cometbft/types"
	"github.com/stretchr/testify/require"
)

func TestSet_Assign(t *testing.T) {
	commitment := rsema1d.Commitment{
		1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16,
		17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32,
	}

	t.Run("EmptySet", func(t *testing.T) {
		valSet := makeValidatorSet(0)

		shardMap := valSet.Assign(commitment, 10)
		require.NotNil(t, shardMap)
		require.Empty(t, shardMap)
	})

	t.Run("ZeroRows", func(t *testing.T) {
		valSet := makeValidatorSet(3)

		shardMap := valSet.Assign(commitment, 0)
		require.NotNil(t, shardMap)
		require.Empty(t, shardMap)
	})

	t.Run("SingleValidator", func(t *testing.T) {
		valSet := makeValidatorSet(1)

		numRows := 10
		shardMap := valSet.Assign(commitment, numRows)
		require.NotNil(t, shardMap)
		require.Len(t, shardMap, 1)

		// All rows should be assigned to the single validator
		for _, rows := range shardMap {
			require.Len(t, rows, numRows)
			// Verify all row indices are present
			for i := range numRows {
				require.Contains(t, rows, i)
			}
		}
	})

	t.Run("Distribution", func(t *testing.T) {
		valSet := makeValidatorSet(100)

		hasher := sha256.New()
		hasher.Write([]byte("distribution-test"))
		var distCommitment rsema1d.Commitment
		copy(distCommitment[:], hasher.Sum(nil))

		totalRows := 16384
		shardMap := valSet.Assign(distCommitment, totalRows)

		require.Len(t, shardMap, len(valSet.Validators), "All validators should be assigned rows")

		expectedPerValidator := totalRows / len(valSet.Validators)
		totalAssigned := 0

		// Track all assigned rows globally to detect cross-validator duplicates
		globalSeen := make(map[int]bool)

		lowest, highest := totalRows, 0
		for val, rows := range shardMap {
			count := len(rows)
			totalAssigned += count

			if count < lowest {
				lowest = count
			}
			if count > highest {
				highest = count
			}

			require.GreaterOrEqual(t, count, expectedPerValidator, "validator %s has too few rows", val.Address)
			require.LessOrEqual(t, count, expectedPerValidator+1, "validator %s has too many rows", val.Address)

			// Verify row indices are unique (within validator and across all validators) and in range
			for _, rowIdx := range rows {
				require.False(t, globalSeen[rowIdx], "duplicate row index %d across validators", rowIdx)
				require.GreaterOrEqual(t, rowIdx, 0)
				require.Less(t, rowIdx, totalRows)
				globalSeen[rowIdx] = true
			}
		}

		require.Equal(t, totalRows, totalAssigned, "All rows should be assigned")
		require.Len(t, globalSeen, totalRows, "All row indices should be assigned exactly once")
		t.Logf("Lowest assignments: %d, Highest assignments: %d", lowest, highest)
	})

	t.Run("Determinism", func(t *testing.T) {
		valSet := makeValidatorSet(3)

		numRows := 50
		firstRun := valSet.Assign(commitment, numRows)
		secondRun := valSet.Assign(commitment, numRows)

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

	// Typical total rows for different blob sizes (K=4096, N=12288, total=16384)
	totalRows := 16384

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
				_ = valSet.Assign(commitment, totalRows)
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
	shardMap := valSet.Assign(commitment, 10)

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
