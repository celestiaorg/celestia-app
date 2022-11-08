package keeper

import (
	"context"
	"fmt"

	"github.com/tendermint/tendermint/libs/log"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/pkg/shares"
	"github.com/celestiaorg/celestia-app/x/blob/types"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

const (
	payForBlobGasDescriptor = "pay for data"

	// GasPerMsgByte is the amount of gas to charge per byte of message data.
	// TODO: extract GasPerMsgByte as a parameter to this module.
	GasPerMsgByte  = 8
	GasPerMsgShare = appconsts.ShareSize * GasPerMsgByte
)

// Keeper handles all the state changes for the blob module.
type Keeper struct {
	cdc codec.BinaryCodec
}

func NewKeeper(cdc codec.BinaryCodec) *Keeper {
	return &Keeper{
		cdc: cdc,
	}
}

func (k Keeper) Logger(ctx sdk.Context) log.Logger {
	return ctx.Logger().With("module", fmt.Sprintf("x/%s", types.ModuleName))
}

// PayForBlob consumes gas based on the message size.
func (k Keeper) PayForBlob(goCtx context.Context, msg *types.MsgPayForBlob) (*types.MsgPayForBlobResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	gasToConsume := uint64(shares.MsgSharesUsed(int(msg.MessageSize)) * GasPerMsgShare)
	ctx.GasMeter().ConsumeGas(gasToConsume, payForBlobGasDescriptor)

	ctx.EventManager().EmitEvent(
		types.NewPayForBlobEvent(sdk.AccAddress(msg.Signer).String(), msg.GetMessageSize()),
	)

	return &types.MsgPayForBlobResponse{}, nil
}
