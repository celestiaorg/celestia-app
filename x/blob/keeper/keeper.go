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
	payForBlobGasDescriptor = "pay for blob"

	// GasPerBlobByte is the amount of gas to charge per byte of blob data.
	// TODO: extract GasPerBlobByte as a parameter to this module.
	GasPerBlobByte  = 8
	GasPerBlobShare = appconsts.ShareSize * GasPerBlobByte
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

// PayForBlob consumes gas based on the blob size.
func (k Keeper) PayForBlob(goCtx context.Context, msg *types.MsgPayForBlob) (*types.MsgPayForBlobResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	gasToConsume := uint64(shares.BlobSharesUsed(int(msg.BlobSize)) * GasPerBlobShare)
	ctx.GasMeter().ConsumeGas(gasToConsume, payForBlobGasDescriptor)

	err := ctx.EventManager().EmitTypedEvent(
		types.NewPayForBlobEvent(sdk.AccAddress(msg.Signer).String(), msg.GetBlobSize()),
	)
	if err != nil {
		return &types.MsgPayForBlobResponse{}, err
	}

	return &types.MsgPayForBlobResponse{}, nil
}
