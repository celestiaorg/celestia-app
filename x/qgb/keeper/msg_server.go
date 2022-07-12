package keeper

import (
	"context"
	"encoding/hex"
	"fmt"
	"math/big"

	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

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
	valset, err := k.GetValsetByNonce(ctx, msg.Nonce)
	if err != nil {
		return nil, err
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

	// Verify if validator is part of the previous valset
	var previousValset *types.Valset
	// TODO add test for case nonce == 1.
	if msg.Nonce == 1 {
		// if the msg.Nonce == 1, the current valset should sign the first valset. Because, it's the first attestation, and there is no prior validator set defined that should sign this change.
		previousValset = valset
	} else {
		previousValset, err = k.GetLastValsetBeforeNonce(ctx, msg.Nonce)
		if err != nil {
			return nil, err
		}
	}
	if !ValidatorPartOfValset(previousValset.Members, validator.EthAddress) {
		return nil, sdkerrors.Wrap(
			types.ErrValidatorNotInValset,
			fmt.Sprintf("validator %s not part of valset %d", validator.Orchestrator, previousValset.Nonce),
		)
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

	// Verify if validator is part of the previous valset
	var previousValset *types.Valset
	// TODO add test for case nonce == 1.
	if msg.Nonce == 1 {
		// if the msg.Nonce == 1, the current valset should sign the first valset. Because, it's the first attestation, and there is no prior validator set defined that should sign this change.
		previousValset, err = k.GetValsetByNonce(ctx, msg.Nonce)
		if err != nil {
			return nil, err
		}
	} else {
		previousValset, err = k.GetLastValsetBeforeNonce(ctx, msg.Nonce)
		if err != nil {
			return nil, err
		}
	}
	if !ValidatorPartOfValset(previousValset.Members, validator.EthAddress) {
		return nil, sdkerrors.Wrap(
			types.ErrValidatorNotInValset,
			fmt.Sprintf("validator %s not part of valset %d", validator.Orchestrator, previousValset.Nonce),
		)
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

func ValidatorPartOfValset(members []types.BridgeValidator, ethAddr string) bool {
	for _, val := range members {
		if val.EthereumAddress == ethAddr {
			return true
		}
	}
	return false
}
