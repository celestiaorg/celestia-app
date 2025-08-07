package keeper

import (
	storetypes "cosmossdk.io/store/types"
	"github.com/celestiaorg/celestia-app/v6/x/minfee/types"
	"github.com/cosmos/cosmos-sdk/codec"
	params "github.com/cosmos/cosmos-sdk/x/params/keeper"
	paramtypes "github.com/cosmos/cosmos-sdk/x/params/types"
)

type Keeper struct {
	cdc            codec.Codec
	storeKey       storetypes.StoreKey
	paramsKeeper   params.Keeper
	legacySubspace paramtypes.Subspace
	authority      string
}

func NewKeeper(
	cdc codec.Codec,
	storeKey storetypes.StoreKey,
	paramsKeeper params.Keeper,
	legacySubspace paramtypes.Subspace,
	authority string,
) *Keeper {
	if !legacySubspace.HasKeyTable() {
		legacySubspace = legacySubspace.WithKeyTable(types.ParamKeyTable())
	}

	return &Keeper{
		cdc:            cdc,
		storeKey:       storeKey,
		paramsKeeper:   paramsKeeper,
		legacySubspace: legacySubspace,
		authority:      authority,
	}
}

// GetParamsKeeper returns the params keeper.
func (k Keeper) GetParamsKeeper() params.Keeper {
	return k.paramsKeeper
}

// GetAuthority returns the minfee module's authority.
func (k Keeper) GetAuthority() string {
	return k.authority
}
