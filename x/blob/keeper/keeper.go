package keeper

import (
	"context"
	"fmt"

	"github.com/celestiaorg/celestia-app/v3/pkg/appconsts"
	v2 "github.com/celestiaorg/celestia-app/v3/pkg/appconsts/v2"
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
	ctx := sdk.UnwrapSDKContext(goCtx)

	// GasPerBlobByte is a versioned variable from version 3 onwards.
	var gasToConsume uint64
	if ctx.BlockHeader().Version.App <= v2.Version {
		gasToConsume = types.GasToConsume(msg.BlobSizes, k.GasPerBlobByte(ctx))
	} else {
		gasToConsume = types.GasToConsume(msg.BlobSizes, appconsts.GasPerBlobByte(appconsts.LatestVersion))
	}

	ctx.GasMeter().ConsumeGas(gasToConsume, payForBlobGasDescriptor)

	err := ctx.EventManager().EmitTypedEvent(
		types.NewPayForBlobsEvent(msg.Signer, msg.BlobSizes, msg.Namespaces),
	)
	if err != nil {
		return &types.MsgPayForBlobsResponse{}, err
	}

	return &types.MsgPayForBlobsResponse{}, nil
}
