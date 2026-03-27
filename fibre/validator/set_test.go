package validator_test

import (
	"fmt"
	"testing"

	"github.com/celestiaorg/celestia-app/v8/fibre/validator"
	"github.com/celestiaorg/celestia-app/v8/pkg/rsema1d"
	"github.com/cometbft/cometbft/crypto/ed25519"
	cmtmath "github.com/cometbft/cometbft/libs/math"
	core "github.com/cometbft/cometbft/types"
	"github.com/stretchr/testify/require"
)

var (
	testLivenessThreshold = cmtmath.Fraction{Numerator: 1, Denominator: 3}
	testCommitment        = rsema1d.Commitment{
		1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16,
		17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32,
	}
)

const (
	testOriginalRows = 4096
	testMinRows      = 148
)

func TestSet_Assign(t *testing.T) {
	t.Run("empty set", func(t *testing.T) {
		valSet := makeValidatorSet(0)
		shardMap := valSet.Assign(testCommitment, 100, 25, 10, testLivenessThreshold)
		require.Empty(t, shardMap)
	})

	t.Run("zero total rows", func(t *testing.T) {
		valSet := makeValidatorSet(3)
		shardMap := valSet.Assign(testCommitment, 0, 0, 10, testLivenessThreshold)
		require.Empty(t, shardMap)
	})

	t.Run("zero min rows", func(t *testing.T) {
		valSet := makeValidatorSet(3)
		shardMap := valSet.Assign(testCommitment, 100, 25, 0, testLivenessThreshold)
		require.Empty(t, shardMap)
	})

	t.Run("single validator", func(t *testing.T) {
		valSet := makeValidatorSet(1)
		shardMap := valSet.Assign(testCommitment, 30, 10, 1, testLivenessThreshold)
		require.Len(t, shardMap, 1)

		for _, rows := range shardMap {
			require.Len(t, rows, 10) // capped at originalRows
		}
	})

	t.Run("equal stakes distribution", func(t *testing.T) {
		valSet := makeValidatorSet(100)
		shardMap := valSet.Assign(testCommitment, 3000, 1000, 1, testLivenessThreshold)
		require.Len(t, shardMap, 100)

		seen := make(map[int]bool)
		for _, rows := range shardMap {
			require.Len(t, rows, 30) // 100 validators × 1% stake × 3 (1/threshold) = 30 rows each
			for _, idx := range rows {
				require.False(t, seen[idx])
				seen[idx] = true
			}
		}
		require.Len(t, seen, 3000)
	})

	t.Run("stake proportional distribution", func(t *testing.T) {
		valSet := makeValidatorSetWithStakes([]int64{1, 2, 3})
		// use larger originalRows so cap doesn't affect proportional distribution
		shardMap := valSet.Assign(testCommitment, 54, 18, 1, testLivenessThreshold)
		require.Len(t, shardMap, 3)

		seen := make(map[int]bool)
		for val, rows := range shardMap {
			// rows = ceil(originalRows * stake / totalStake / livenessThreshold), capped at originalRows
			expected := int((int64(18)*val.VotingPower*3 + 5) / 6)
			expected = min(expected, 18) // cap at originalRows
			require.Len(t, rows, expected)
			for _, idx := range rows {
				require.False(t, seen[idx])
				seen[idx] = true
			}
		}
	})

	t.Run("min rows floor", func(t *testing.T) {
		valSet := makeValidatorSetWithStakes([]int64{1, 2, 3})
		// minRows=5 ensures everyone gets at least 5, causing wrap-around
		// Note: minRows must be <= originalRows
		shardMap := valSet.Assign(testCommitment, 18, 6, 5, testLivenessThreshold)
		require.Len(t, shardMap, 3)

		for _, rows := range shardMap {
			require.GreaterOrEqual(t, len(rows), 5)
			for _, idx := range rows {
				require.Less(t, idx, 18)
			}
		}
	})

	t.Run("determinism", func(t *testing.T) {
		valSet := makeValidatorSet(3)
		first := valSet.Assign(testCommitment, 51, 17, 5, testLivenessThreshold)
		second := valSet.Assign(testCommitment, 51, 17, 5, testLivenessThreshold)

		for val, rows := range first {
			require.Equal(t, rows, second[val])
		}
	})
}

func TestShardMap_Verify(t *testing.T) {
	valSet := makeValidatorSet(3)
	shardMap := valSet.Assign(testCommitment, 12, 4, 1, testLivenessThreshold)

	t.Run("valid", func(t *testing.T) {
		for val, rows := range shardMap {
			indices := make([]uint32, len(rows))
			for i, idx := range rows {
				indices[i] = uint32(idx)
			}
			require.NoError(t, shardMap.Verify(val, indices))
		}
	})

	t.Run("too few rows", func(t *testing.T) {
		for val, rows := range shardMap {
			if len(rows) < 2 {
				continue
			}
			require.ErrorContains(t, shardMap.Verify(val, []uint32{uint32(rows[0])}), "expected")
			break
		}
	})

	t.Run("wrong row", func(t *testing.T) {
		for val, rows := range shardMap {
			if len(rows) == 0 {
				continue
			}
			indices := make([]uint32, len(rows))
			for i, idx := range rows {
				indices[i] = uint32(idx)
			}
			indices[0] = 999
			require.ErrorContains(t, shardMap.Verify(val, indices), "not assigned")
			break
		}
	})
}

func TestSet_Select(t *testing.T) {
	t.Run("empty set", func(t *testing.T) {
		selected := makeValidatorSet(0).Select(testOriginalRows, testMinRows, testLivenessThreshold)
		require.Nil(t, selected)
	})

	t.Run("single validator", func(t *testing.T) {
		selected := makeValidatorSet(1).Select(testOriginalRows, testMinRows, testLivenessThreshold)
		require.Len(t, selected, 1)
	})

	t.Run("two validators", func(t *testing.T) {
		selected := makeValidatorSet(2).Select(testOriginalRows, testMinRows, testLivenessThreshold)
		require.Len(t, selected, 2)
	})

	t.Run("returns all validators", func(t *testing.T) {
		for _, size := range []int{1, 2, 5, 20, 100} {
			t.Run(fmt.Sprintf("%d", size), func(t *testing.T) {
				valSet := makeValidatorSet(size)
				selected := valSet.Select(testOriginalRows, testMinRows, testLivenessThreshold)
				require.Len(t, selected, size)

				// verify all original validators are present
				selectedAddrs := make(map[string]bool)
				for _, sv := range selected {
					selectedAddrs[sv.Address.String()] = true
				}
				for _, v := range valSet.Validators {
					require.True(t, selectedAddrs[v.Address.String()], "validator %s missing from Select output", v.Address)
				}
			})
		}
	})

	t.Run("stake distributions", func(t *testing.T) {
		cases := []struct {
			name   string
			stakes []int64
		}{
			{"power_law", []int64{40, 20, 15, 10, 7, 5, 3}},
			{"one_dominant", []int64{50, 10, 10, 10, 10, 10}},
			{"two_dominant", []int64{25, 25, 10, 10, 10, 10, 10}},
			{"long_tail", append([]int64{15, 15, 10, 10}, makeStakes(16, 3)...)},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				valSet := makeValidatorSetWithStakes(tc.stakes)
				selected := valSet.Select(testOriginalRows, testMinRows, testLivenessThreshold)
				require.Len(t, selected, len(tc.stakes))

				// verify all original validators are present
				selectedAddrs := make(map[string]bool)
				for _, sv := range selected {
					selectedAddrs[sv.Address.String()] = true
				}
				for _, v := range valSet.Validators {
					require.True(t, selectedAddrs[v.Address.String()], "validator %s missing from Select output", v.Address)
				}
			})
		}
	})
}

// TestSelect_NoOverlapBeforeSplitIdx verifies that when ShardMap has duplicates, Select returns validators ordered such that
// a prefix exists with no overlapping rows covering enough for reconstruction
func TestSelect_NoOverlapBeforeSplitIdx(t *testing.T) {
	// realistic stake distribution: large validators cause row overflow and duplicates
	stakes := append([]int64{20, 15, 10}, makeStakes(97, 1)...)
	valSet := makeValidatorSetWithStakes(stakes)

	totalRows := testOriginalRows * 4
	shardMap := valSet.Assign(testCommitment, totalRows, testOriginalRows, testMinRows, testLivenessThreshold)

	// verify duplicates exist (test precondition)
	hasDuplicates := func() bool {
		seen := make(map[int]bool)
		for _, rows := range shardMap {
			for _, row := range rows {
				if seen[row] {
					return true
				}
				seen[row] = true
			}
		}
		return false
	}
	require.True(t, hasDuplicates(), "test requires duplicate rows")

	selected := valSet.Select(testOriginalRows, testMinRows, testLivenessThreshold)

	// verify no overlaps until we have enough unique rows for reconstruction
	seen := make(map[int]bool)
	for _, sv := range selected {
		for _, row := range shardMap[sv.Validator] {
			if seen[row] {
				require.GreaterOrEqual(t, len(seen), testOriginalRows, "overlap before reconstruction threshold")
				return
			}
			seen[row] = true
		}
	}
	require.GreaterOrEqual(t, len(seen), testOriginalRows)
}

// TestSelect_AssignRelationship verifies that walking validators in Select order
// accumulates enough unique rows from Assign for reconstruction.
func TestSelect_AssignRelationship(t *testing.T) {
	cases := []struct {
		name   string
		stakes []int64
	}{
		{"single_validator", []int64{100}},
		{"two_validators", []int64{50, 50}},
		{"equal_20", makeStakes(20, 5)},
		{"power_law", []int64{40, 20, 15, 10, 7, 5, 3}},
		{"one_dominant", []int64{50, 10, 10, 10, 10, 10}},
		{"long_tail", append([]int64{30, 20, 10}, makeStakes(40, 1)...)},
		{"liveness_ceil", makeStakes(4, 1)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			valSet := makeValidatorSetWithStakes(tc.stakes)
			totalRows := testOriginalRows * 4

			shardMap := valSet.Assign(testCommitment, totalRows, testOriginalRows, testMinRows, testLivenessThreshold)
			selected := valSet.Select(testOriginalRows, testMinRows, testLivenessThreshold)

			// Walk Select order, accumulating unique rows until we have enough
			unique := make(map[int]struct{})
			for _, sv := range selected {
				for _, row := range shardMap[sv.Validator] {
					unique[row] = struct{}{}
				}
				if len(unique) >= testOriginalRows {
					break
				}
			}

			require.GreaterOrEqual(t, len(unique), testOriginalRows)
		})
	}
}

func makeValidatorSet(n int) validator.Set {
	validators := make([]*core.Validator, n)
	for i := range n {
		validators[i] = core.NewValidator(ed25519.GenPrivKey().PubKey(), 1)
	}
	return validator.Set{
		ValidatorSet: core.NewValidatorSet(validators),
		Height:       100,
	}
}

func makeValidatorSetWithStakes(stakes []int64) validator.Set {
	validators := make([]*core.Validator, len(stakes))
	for i, stake := range stakes {
		validators[i] = core.NewValidator(ed25519.GenPrivKey().PubKey(), stake)
	}
	return validator.Set{
		ValidatorSet: core.NewValidatorSet(validators),
		Height:       100,
	}
}

func makeStakes(count int, each int64) []int64 {
	stakes := make([]int64, count)
	for i := range stakes {
		stakes[i] = each
	}
	return stakes
}
