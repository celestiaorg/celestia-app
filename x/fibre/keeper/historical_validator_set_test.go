package keeper

import (
	"context"
	"testing"

	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/require"
)

// countingStakingKeeper reports whether each height is stored and counts the
// reads, which is what the memo is there to keep down.
type countingStakingKeeper struct {
	stored map[int64]bool
	reads  int
}

func (s *countingStakingKeeper) GetHistoricalInfo(_ context.Context, height int64) (stakingtypes.HistoricalInfo, error) {
	s.reads++
	if !s.stored[height] {
		return stakingtypes.HistoricalInfo{}, stakingtypes.ErrNoHistoricalInfo
	}
	return stakingtypes.HistoricalInfo{}, nil
}

// contextAtBlock returns a context carrying only what the assertion reads.
func contextAtBlock(height int64) sdk.Context {
	return sdk.NewContext(nil, cmtproto.Header{Height: height}, false, nil)
}

func TestValidateHistoricalValidatorSet(t *testing.T) {
	staking := &countingStakingKeeper{stored: map[int64]bool{7: true}}
	k := Keeper{stakingKeeper: staking, historicalHeights: newHistoricalHeightCache()}

	require.NoError(t, k.ValidateHistoricalValidatorSet(contextAtBlock(100), 7))
	require.Equal(t, 1, staking.reads)

	// Within one block the answer cannot change: staking writes an entry once
	// per height and prunes only at the start of a block.
	require.NoError(t, k.ValidateHistoricalValidatorSet(contextAtBlock(100), 7))
	require.Equal(t, 1, staking.reads, "a second transaction at the same block must not read again")

	// A pruned height is the whole point: it must be reported, and reported
	// the way the sequential verifier reports it.
	err := k.ValidateHistoricalValidatorSet(contextAtBlock(100), 3)
	require.ErrorIs(t, err, stakingtypes.ErrNoHistoricalInfo)
	require.Equal(t, 2, staking.reads)
	require.ErrorIs(t, k.ValidateHistoricalValidatorSet(contextAtBlock(100), 3), stakingtypes.ErrNoHistoricalInfo)
	require.Equal(t, 2, staking.reads, "the negative answer is memoized too")

	// The next block is a new question: the height the last block resolved may
	// have been pruned since, so the memo must not answer for it.
	staking.stored[7] = false
	require.NoError(t, k.ValidateHistoricalValidatorSet(contextAtBlock(100), 7))
	require.ErrorIs(t, k.ValidateHistoricalValidatorSet(contextAtBlock(101), 7), stakingtypes.ErrNoHistoricalInfo)
	require.Equal(t, 3, staking.reads)
}
