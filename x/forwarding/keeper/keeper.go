package keeper

import (
	"github.com/celestiaorg/celestia-app/v6/x/forwarding/types"
	"github.com/cosmos/cosmos-sdk/codec"
)

// Keeper maintains state for the forwarding module.
// The module currently has no state; this exists to satisfy future wiring.
type Keeper struct {
	cdc codec.BinaryCodec

	router types.MessageRouter
}

// NewKeeper creates a new forwarding Keeper instance.
func NewKeeper(cdc codec.Codec) Keeper {
	return Keeper{
		cdc: cdc,
	}
}
