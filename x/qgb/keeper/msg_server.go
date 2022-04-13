package keeper

import (
	"context"
	"encoding/hex"
	"fmt"
	"github.com/celestiaorg/celestia-app/x/qgb/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

type msgServer struct {
	Keeper
}

// NewMsgServerImpl returns an implementation of the gov MsgServer interface
// for the provided Keeper.
func NewMsgServerImpl(keeper Keeper) types.MsgServer {
	return &msgServer{Keeper: keeper}
}

// ValsetConfirm handles MsgValsetConfirm
func (k msgServer) ValsetConfirm(
	c context.Context,
	msg *types.MsgValsetConfirm,
) (*types.MsgValsetConfirmResponse, error) {
	ctx := sdk.UnwrapSDKContext(c)
	valset := k.GetValset(ctx, msg.Nonce)
	if valset == nil {
		return nil, sdkerrors.Wrap(types.ErrInvalid, "couldn't find valset")
	}

	orchaddr, err := sdk.AccAddressFromBech32(msg.Orchestrator)
	if err != nil {
		return nil, sdkerrors.Wrap(types.ErrInvalid, "acc address invalid")
	}
	err = k.confirmHandlerCommon(ctx, msg.EthAddress, msg.Orchestrator, msg.Signature)
	if err != nil {
		return nil, err
	}
	// persist signature
	if k.GetValsetConfirm(ctx, msg.Nonce, orchaddr) != nil {
		return nil, sdkerrors.Wrap(types.ErrDuplicate, "signature duplicate")
	}
	key := k.SetValsetConfirm(ctx, *msg)

	ctx.EventManager().EmitEvent(
		sdk.NewEvent(
			sdk.EventTypeMessage,
			sdk.NewAttribute(sdk.AttributeKeyModule, msg.Type()),
			sdk.NewAttribute(types.AttributeKeyValsetConfirmKey, string(key)),
		),
	)

	return &types.MsgValsetConfirmResponse{}, nil
}

// DataCommitmentConfirm handles MsgDataCommitmentConfirm
func (k msgServer) DataCommitmentConfirm(
	c context.Context,
	msg *types.MsgDataCommitmentConfirm,
) (*types.MsgDataCommitmentConfirmResponse, error) {
	ctx := sdk.UnwrapSDKContext(c)
	sigBytes, err := hex.DecodeString(msg.Signature)
	if err != nil {
		return nil, sdkerrors.Wrap(types.ErrInvalid, "signature decoding")
	}

	// verify validator address
	validatorAddress, err := sdk.AccAddressFromBech32(msg.ValidatorAddress)
	if err != nil {
		return nil, sdkerrors.Wrap(types.ErrInvalid, "validator address invalid")
	}
	validator, found := k.StakingKeeper.GetValidatorByOrchestrator(ctx, validatorAddress)
	if !found {
		return nil, sdkerrors.Wrap(types.ErrUnknown, "validator")
	}
	if err := sdk.VerifyAddressFormat(validator.GetOperator()); err != nil {
		return nil, sdkerrors.Wrapf(err, "discovered invalid validator address for validator %v", validatorAddress)
	}

	// verify ethereum address
	ethAddress, err := types.NewEthAddress(msg.EthAddress)
	if err != nil {
		return nil, sdkerrors.Wrap(types.ErrInvalid, "invalid eth address")
	}
	err = types.ValidateEthereumSignature([]byte(msg.Commitment), sigBytes, *ethAddress)
	if err != nil {
		return nil,
			sdkerrors.Wrap(
				types.ErrInvalid,
				fmt.Sprintf(
					"signature verification failed expected sig by %s with checkpoint %s found %s",
					ethAddress,
					msg.Commitment,
					msg.Signature,
				),
			)
	}
	k.StakingKeeper.GetValidator(ctx, validator.GetOperator())
	// TODO check if this comparison is right
	if validator.EthAddress != ethAddress.GetAddress() {
		return nil, sdkerrors.Wrap(types.ErrInvalid, "submitted eth address does not match delegate eth address")
	}

	k.SetDataCommitmentConfirm(ctx, *msg)

	ctx.EventManager().EmitEvent(
		sdk.NewEvent(
			sdk.EventTypeMessage,
			sdk.NewAttribute(sdk.AttributeKeyModule, msg.Type()),
			sdk.NewAttribute(types.AttributeKeyDataCommitmentConfirmKey, msg.String()),
		),
	)

	return &types.MsgDataCommitmentConfirmResponse{}, nil
}

// confirmHandlerCommon is an internal function that provides common code for processing claim messages
func (k msgServer) confirmHandlerCommon(ctx sdk.Context, ethAddress string, orchestrator string, signature string) error {
	_, err := hex.DecodeString(signature)
	if err != nil {
		return sdkerrors.Wrap(types.ErrInvalid, "signature decoding")
	}

	submittedEthAddress, err := types.NewEthAddress(ethAddress)
	if err != nil {
		return sdkerrors.Wrap(types.ErrInvalid, "invalid eth address")
	}

	orchaddr, err := sdk.AccAddressFromBech32(orchestrator)
	if err != nil {
		return sdkerrors.Wrap(types.ErrInvalid, "orch acc address invalid")
	}

	validator, found := k.StakingKeeper.GetValidatorByOrchestrator(ctx, orchaddr)
	if !found {
		return sdkerrors.Wrap(types.ErrUnknown, "validator")
	}
	if err := sdk.VerifyAddressFormat(validator.GetOperator()); err != nil {
		return sdkerrors.Wrapf(err, "discovered invalid validator address for orchestrator %v", orchaddr)
	}

	// TODO check if this makes sense
	if validator.EthAddress != submittedEthAddress.GetAddress() {
		return sdkerrors.Wrap(types.ErrInvalid, "submitted eth address does not match delegate eth address")
	}
	return nil
}
