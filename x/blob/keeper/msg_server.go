package keeper

import (
	"context"

	"github.com/celestiaorg/celestia-app/pkg/shares"
	"github.com/celestiaorg/celestia-app/x/blob/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

var _ types.MsgServer = msgServer{}

type msgServer struct {
	Keeper
}

// NewMsgServerImpl returns an implementation of the blob MsgServer interface
// for the provided Keeper.
func NewMsgServerImpl(keeper Keeper) types.MsgServer {
	return &msgServer{Keeper: keeper}
}

// PayForBlob consumes gas based on the blob size.
func (k Keeper) PayForBlob(goCtx context.Context, msg *types.MsgPayForBlob) (*types.MsgPayForBlobResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	gasToConsume := uint64(shares.BlobSharesUsed(int(msg.BlobSize)) * GasPerBlobShare)
	ctx.GasMeter().ConsumeGas(gasToConsume, payForBlobGasDescriptor)

	ctx.EventManager().EmitEvent(
		types.NewPayForBlobEvent(sdk.AccAddress(msg.Signer).String(), msg.GetBlobSize()),
	)

	return &types.MsgPayForBlobResponse{}, nil
}
