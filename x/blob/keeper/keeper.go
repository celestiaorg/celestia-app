package keeper

import (
	"context"

	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	paramtypes "github.com/cosmos/cosmos-sdk/x/params/types"

	"github.com/celestiaorg/celestia-app/v4/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v4/x/blob/types"
)

const (
	payForBlobGasDescriptor = "pay for blob"
)

// Keeper handles all the state changes for the blob module.
type Keeper struct {
	cdc        codec.Codec
	paramStore paramtypes.Subspace
}

func NewKeeper(
	cdc codec.Codec,
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

// PayForBlobs consumes gas based on the blob sizes in the MsgPayForBlobs.
func (k Keeper) PayForBlobs(goCtx context.Context, msg *types.MsgPayForBlobs) (*types.MsgPayForBlobsResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	gasToConsume := types.GasToConsume(msg.BlobSizes, appconsts.DefaultGasPerBlobByte)

	ctx.GasMeter().ConsumeGas(gasToConsume, payForBlobGasDescriptor)

	if err := ctx.EventManager().EmitTypedEvent(
		types.NewPayForBlobsEvent(msg.Signer, msg.BlobSizes, msg.Namespaces),
	); err != nil {
		return &types.MsgPayForBlobsResponse{}, err
	}

	return &types.MsgPayForBlobsResponse{}, nil
}
