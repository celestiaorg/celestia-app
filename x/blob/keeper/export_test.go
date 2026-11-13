package keeper

import storetypes "cosmossdk.io/store/types"

// StoreKey exposes the blob store key to tests.
func (k Keeper) StoreKey() storetypes.StoreKey {
	return k.storeKey
}
