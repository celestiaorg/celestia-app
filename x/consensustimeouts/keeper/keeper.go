package keeper

import (
	storetypes "cosmossdk.io/store/types"
	"github.com/celestiaorg/celestia-app/v9/x/consensustimeouts/types"
	"github.com/cosmos/cosmos-sdk/codec"
)

// Keeper is the x/consensustimeouts state keeper. It owns the module's
// Params row and gates updates through the authority address.
type Keeper struct {
	cdc       codec.BinaryCodec
	storeKey  storetypes.StoreKey
	authority string
}

// NewKeeper constructs a Keeper for the consensustimeouts module.
func NewKeeper(cdc codec.BinaryCodec, storeKey storetypes.StoreKey, authority string) *Keeper {
	return &Keeper{cdc: cdc, storeKey: storeKey, authority: authority}
}

// GetAuthority returns the bech32 address authorized to update module params.
func (k Keeper) GetAuthority() string { return k.authority }

var (
	_ types.MsgServer   = (*Keeper)(nil)
	_ types.QueryServer = (*Keeper)(nil)
)
