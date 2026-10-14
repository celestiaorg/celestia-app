package keeper

import (
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	lru "github.com/hashicorp/golang-lru/v2"
)

// historicalHeightCacheSize bounds the memo. A block references a handful of
// heights, but CheckTx serves whatever heights it is sent.
const historicalHeightCacheSize = 1024

// historicalHeightKey is one answer. The block is part of it because staking
// prunes historical info as the chain advances, so an answer is only good for
// the block it was read at.
type historicalHeightKey struct {
	block   int64
	promise int64
}

// newHistoricalHeightCache returns the per-block historical height memo.
func newHistoricalHeightCache() *lru.Cache[historicalHeightKey, bool] {
	cache, err := lru.New[historicalHeightKey, bool](historicalHeightCacheSize)
	if err != nil {
		panic(err)
	}
	return cache
}

// ValidateHistoricalValidatorSet reports whether height still resolves to a
// historical validator set, with the error the signature verifier itself would
// report if it did not.
//
// The signature cache is keyed by the certificate, which commits to the
// promise's height but not to the availability of the set it resolves to, and
// an entry outlives that availability. This keeps the one state read the
// verdict depends on on every path, so a warm and a cold node agree.
//
// The answer is memoized per block: staking writes historical info once per
// height and only ever deletes it, at the start of a block, so within a block
// neither answer can change.
func (k Keeper) ValidateHistoricalValidatorSet(ctx sdk.Context, height int64) error {
	key := historicalHeightKey{block: ctx.BlockHeight(), promise: height}
	resolved, memoized := k.historicalHeights.Get(key)
	if !memoized {
		_, err := k.stakingKeeper.GetHistoricalInfo(ctx, height)
		resolved = err == nil
		k.historicalHeights.Add(key, resolved)
	}
	if !resolved {
		// The error, and the message, the sequential verifier reports when it
		// reads the height itself.
		return errorsmod.Wrapf(stakingtypes.ErrNoHistoricalInfo,
			"failed to get historical validator set at height %d", height)
	}
	return nil
}
