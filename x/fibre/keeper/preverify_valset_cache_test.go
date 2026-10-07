package keeper

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/require"
)

type countingHistoricalKeeper struct {
	calls atomic.Int64
}

func (k *countingHistoricalKeeper) GetHistoricalInfo(context.Context, int64) (stakingtypes.HistoricalInfo, error) {
	k.calls.Add(1)
	return stakingtypes.HistoricalInfo{}, nil
}

func TestPreverifyValsetCacheIsScopedToOnePass(t *testing.T) {
	staking := &countingHistoricalKeeper{}
	keeper := Keeper{stakingKeeper: staking}
	ctx := WithPreverifyValsetCache(sdk.Context{}.WithContext(context.Background()))
	const workers = 32
	sets := make([]*convertedValset, workers)
	errs := make([]error, workers)
	var wg sync.WaitGroup
	for i := range workers {
		wg.Go(func() {
			sets[i], errs[i] = keeper.validatorSetForSignatures(ctx, 7)
		})
	}
	wg.Wait()
	for i := range workers {
		require.NoError(t, errs[i])
		require.Same(t, sets[0], sets[i])
	}
	require.EqualValues(t, 1, staking.calls.Load())

	otherPass := WithPreverifyValsetCache(sdk.Context{}.WithContext(context.Background()))
	_, err := keeper.validatorSetForSignatures(otherPass, 7)
	require.NoError(t, err)
	require.EqualValues(t, 2, staking.calls.Load())
}
