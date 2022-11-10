package keeper

import (
	"context"
	"fmt"

	"github.com/tendermint/tendermint/libs/log"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/pkg/shares"
	"github.com/celestiaorg/celestia-app/x/blob/types"
	"github.com/cosmos/cosmos-sdk/codec"
	storetypes "github.com/cosmos/cosmos-sdk/store/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	paramtypes "github.com/cosmos/cosmos-sdk/x/params/types"
)

const (
	payForDataGasDescriptor = "pay for data"

	// GasPerMsgByte is the amount of gas to charge per byte of message data.
	// TODO: extract GasPerMsgByte as a parameter to this module.
	GasPerMsgByte  = 8
	GasPerMsgShare = appconsts.ShareSize * GasPerMsgByte
)

// Keeper handles all the state changes for the blob module.
type Keeper struct {
	cdc        codec.BinaryCodec
	storeKey   storetypes.StoreKey
	memKey     storetypes.StoreKey
	paramStore paramtypes.Subspace
}

func NewKeeper(
	cdc codec.BinaryCodec,
	storeKey,
	memKey storetypes.StoreKey,
	ps paramtypes.Subspace,
) *Keeper {
	if !ps.HasKeyTable() {
		ps = ps.WithKeyTable(types.ParamKeyTable())
	}

	return &Keeper{
		cdc:        cdc,
		storeKey:   storeKey,
		memKey:     memKey,
		paramStore: ps,
	}
}

func (k Keeper) Logger(ctx sdk.Context) log.Logger {
	return ctx.Logger().With("module", fmt.Sprintf("x/%s", types.ModuleName))
}

// PayForData consumes gas based on the message size.
func (k Keeper) PayForData(goCtx context.Context, msg *types.MsgPayForData) (*types.MsgPayForDataResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	gasToConsume := uint64(shares.MsgSharesUsed(int(msg.MessageSize)) * GasPerMsgShare)
	ctx.GasMeter().ConsumeGas(gasToConsume, payForDataGasDescriptor)

	ctx.EventManager().EmitEvent(
		types.NewPayForDataEvent(sdk.AccAddress(msg.Signer).String(), msg.GetMessageSize()),
	)

	return &types.MsgPayForDataResponse{}, nil
}
