package keeper

import (
	"context"

	"cosmossdk.io/errors"
	storetypes "cosmossdk.io/store/types"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	paramtypes "github.com/cosmos/cosmos-sdk/x/params/types"

	"github.com/celestiaorg/celestia-app/v4/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v4/x/blob/types"
)

const (
	payForBlobGasDescriptor = "pay for blob"
)

// Keeper handles all the state changes for the blob module.
type Keeper struct {
	cdc            codec.Codec
	storeKey       storetypes.StoreKey
	legacySubspace paramtypes.Subspace
	authority      string
}

func NewKeeper(
	cdc codec.Codec,
	storeKey storetypes.StoreKey,
	legacySubspace paramtypes.Subspace,
	authority string,
) *Keeper {
	if !legacySubspace.HasKeyTable() {
		legacySubspace = legacySubspace.WithKeyTable(types.ParamKeyTable())
	}

	return &Keeper{
		cdc:            cdc,
		storeKey:       storeKey,
		legacySubspace: legacySubspace,
		authority:      authority,
	}
}

// GetAuthority returns the client submodule's authority.
func (k Keeper) GetAuthority() string {
	return k.authority
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

// UpdateBlobParams updates blob module parameters.
func (k Keeper) UpdateBlobParams(goCtx context.Context, msg *types.MsgUpdateBlobParams) (*types.MsgUpdateBlobParamsResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	// ensure that the sender has the authority to update the parameters.
	if msg.Authority != k.GetAuthority() {
		return nil, errors.Wrapf(sdkerrors.ErrUnauthorized, "invalid authority: expected: %s, got: %s", k.authority, msg.Authority)
	}

	if err := msg.Params.Validate(); err != nil {
		return nil, errors.Wrapf(sdkerrors.ErrInvalidRequest, "invalid parameters: %s", err)
	}

	k.SetParams(ctx, msg.Params)

	// Emit an event indicating successful parameter update.
	if err := ctx.EventManager().EmitTypedEvent(
		types.NewUpdateBlobParamsEvent(msg.Authority, msg.Params),
	); err != nil {
		return nil, err
	}

	return &types.MsgUpdateBlobParamsResponse{}, nil
}
