package app

import (
	"context"

	corestore "cosmossdk.io/core/store"
	stakingkeeper "github.com/cosmos/cosmos-sdk/x/staking/keeper"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
)

// fibreStakingKeeper adds the HasHistoricalInfo fibre needs, which the SDK
// fork's staking keeper lacks. Delete it once staking provides one.
type fibreStakingKeeper struct {
	*stakingkeeper.Keeper
	storeService corestore.KVStoreService
}

// HasHistoricalInfo reads the key GetHistoricalInfo reads and skips only the decode.
func (k fibreStakingKeeper) HasHistoricalInfo(ctx context.Context, height int64) (bool, error) {
	return k.storeService.OpenKVStore(ctx).Has(stakingtypes.GetHistoricalInfoKey(height))
}
