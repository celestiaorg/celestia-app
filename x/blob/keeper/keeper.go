package keeper

import (
	"context"
	"fmt"

	"github.com/celestiaorg/celestia-app/v3/x/blob/types"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	paramtypes "github.com/cosmos/cosmos-sdk/x/params/types"
	"github.com/tendermint/tendermint/libs/log"
)

const (
	payForBlobGasDescriptor = "pay for blob"
)

// Keeper handles all the state changes for the blob module.
type Keeper struct {
	cdc        codec.BinaryCodec
	paramStore paramtypes.Subspace
}

func NewKeeper(
	cdc codec.BinaryCodec,
	ps paramtypes.Subspace,
) *Keeper {
	if !ps.HasKeyTable() {
		ps = ps.WithKeyTable(types.ParamKeyTable())
	}

	return &Keeper{
		cdc:        cdc,
		paramStore: ps,
	}
}

func (k Keeper) Logger(ctx sdk.Context) log.Logger {
	return ctx.Logger().With("module", fmt.Sprintf("x/%s", types.ModuleName))
}

// PayForBlobs consumes gas based on the blob sizes in the MsgPayForBlobs.
func (k Keeper) PayForBlobs(goCtx context.Context, msg *types.MsgPayForBlobs) (*types.MsgPayForBlobsResponse, error) {
	// ctx := sdk.UnwrapSDKContext(goCtx)

	// gasToConsume := types.GasToConsume(msg.BlobSizes, k.GasPerBlobByte(ctx))
	// ctx.GasMeter().ConsumeGas(gasToConsume, payForBlobGasDescriptor)

	// err := ctx.EventManager().EmitTypedEvent(
	// 	types.NewPayForBlobsEvent(msg.Signer, msg.BlobSizes, msg.Namespaces),
	// )
	// if err != nil {
	// 	return &types.MsgPayForBlobsResponse{}, err
	// }

	return &types.MsgPayForBlobsResponse{}, nil
}
