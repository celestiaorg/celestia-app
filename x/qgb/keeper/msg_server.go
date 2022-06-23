package keeper

import (
	"context"
	"encoding/hex"
	"fmt"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"math/big"

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

// ValsetConfirm handles MsgValsetConfirm.
func (k msgServer) ValsetConfirm(
	c context.Context,
	msg *types.MsgValsetConfirm,
) (*types.MsgValsetConfirmResponse, error) {
	ctx := sdk.UnwrapSDKContext(c)

	// Get valset by nonce
	at := k.GetAttestationByNonce(ctx, msg.Nonce)
	if at == nil {
		return nil, types.ErrAttestationNotFound
	}
	if at.Type() != types.ValsetRequestType {
		return nil, sdkerrors.Wrap(types.ErrAttestationNotValsetRequest, "attestation is not a valset request")
	}

	valset, ok := at.(*types.Valset)
	if !ok {
		return nil, sdkerrors.Wrap(types.ErrAttestationNotValsetRequest, "couldn't cast attestation to valset")
	}

	// Get orchestrator account from message
	orchaddr, err := sdk.AccAddressFromBech32(msg.Orchestrator)
	if err != nil {
		return nil, sdkerrors.Wrap(types.ErrInvalid, "acc address invalid")
	}

	// Verify if validator exists
	validator, found := k.StakingKeeper.GetValidatorByOrchestrator(ctx, orchaddr)
	if !found {
		return nil, sdkerrors.Wrap(types.ErrUnknown, "validator")
	}
	if err := sdk.VerifyAddressFormat(validator.GetOperator()); err != nil {
		return nil, sdkerrors.Wrapf(err, "discovered invalid validator address for orchestrator %v", orchaddr)
	}

	// Verify ethereum address match
	submittedEthAddress, err := stakingtypes.NewEthAddress(msg.EthAddress)
	if err != nil {
		return nil, sdkerrors.Wrap(types.ErrInvalid, "invalid eth address")
	}
	if validator.EthAddress != submittedEthAddress.GetAddress() {
		return nil, sdkerrors.Wrap(types.ErrInvalid, "signing eth address does not match delegate eth address")
	}

	// Verify if signature is correct
	bytesSignature, err := hex.DecodeString(msg.Signature)
	if err != nil {
		return nil, sdkerrors.Wrap(types.ErrInvalid, "signature decoding")
	}
	signBytes, err := valset.SignBytes(types.BridgeId)
	if err != nil {
		return nil, err
	}
	err = types.ValidateEthereumSignature(signBytes.Bytes(), bytesSignature, *submittedEthAddress)
	if err != nil {
		return nil, sdkerrors.Wrap(
			types.ErrInvalid,
			fmt.Sprintf(
				"signature verification failed expected sig by %s for valset nonce %d found %s",
				submittedEthAddress.GetAddress(),
				msg.Nonce,
				msg.Signature,
			),
		)
	}

	// Check if the signature was already posted
	if k.GetValsetConfirm(ctx, msg.Nonce, orchaddr) != nil {
		return nil, sdkerrors.Wrap(types.ErrDuplicate, "signature duplicate")
	}

	// Persist signature
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

// DataCommitmentConfirm handles MsgDataCommitmentConfirm.
func (k msgServer) DataCommitmentConfirm(
	c context.Context,
	msg *types.MsgDataCommitmentConfirm,
) (*types.MsgDataCommitmentConfirmResponse, error) {
	ctx := sdk.UnwrapSDKContext(c)
	sigBytes, err := hex.DecodeString(msg.Signature)
	if err != nil {
		return nil, sdkerrors.Wrap(types.ErrInvalid, "signature decoding")
	}

	// Verify validator address
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

	// Verify ethereum address
	ethAddress, err := stakingtypes.NewEthAddress(msg.EthAddress)
	if err != nil {
		return nil, sdkerrors.Wrap(types.ErrInvalid, "invalid eth address")
	}
	if validator.EthAddress != ethAddress.GetAddress() {
		return nil, sdkerrors.Wrap(types.ErrInvalid, "submitted eth address does not match delegate eth address")
	}

	// Verify signature
	commitment, err := hex.DecodeString(msg.Commitment)
	if err != nil {
		return nil, err
	}
	hash := types.DataCommitmentTupleRootSignBytes(types.BridgeId, big.NewInt(int64(msg.Nonce)), commitment)
	err = types.ValidateEthereumSignature(hash.Bytes(), sigBytes, *ethAddress)
	if err != nil {
		return nil, sdkerrors.Wrap(
			types.ErrInvalid,
			fmt.Sprintf(
				"signature verification failed expected sig by %s with checkpoint %s found %s",
				ethAddress.GetAddress(),
				msg.Commitment,
				msg.Signature,
			),
		)
	}

	// Check if the signature was already posted
	if k.GetDataCommitmentConfirm(ctx, msg.EndBlock, msg.BeginBlock, validatorAddress) != nil {
		return nil, sdkerrors.Wrap(types.ErrDuplicate, "signature duplicate")
	}

	// Persist signature
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
