package keeper

import (
	"context"
	"encoding/hex"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"

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
	valset, found, err := k.GetValsetByNonce(ctx, msg.Nonce)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, sdkerrors.Wrap(types.ErrAttestationNotFound, "valset attestation for nonce not found")
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
		// if the msg.Nonce == 1, the current valset should sign the first valset.
		// Because, it's the first attestation, and there is no prior validator set defined
		// that should sign this change.
		// In fact, the first nonce should never be signed. Because, the first attestation, in the case
		// where the `earliest` flag is specified when deploying the contract, will be relayed as part of
		// the deployment of the QGB contract.
		// It will be signed temporarily for now.
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
	if !common.IsHexAddress(msg.EthAddress) {
		return nil, sdkerrors.Wrap(stakingtypes.ErrEthAddressNotHex, "ethereum address")
	}
	submittedEthAddress := common.HexToAddress(msg.EthAddress)
	if validator.EthAddress != submittedEthAddress.Hex() {
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
	err = types.ValidateEthereumSignature(signBytes.Bytes(), bytesSignature, submittedEthAddress)
	if err != nil {
		return nil, sdkerrors.Wrap(
			types.ErrInvalid,
			fmt.Sprintf(
				"signature verification failed expected sig by %s for valset nonce %d found %s",
				submittedEthAddress.Hex(),
				msg.Nonce,
				msg.Signature,
			),
		)
	}

	// Check if the signature was already posted
	_, found, err = k.GetValsetConfirm(ctx, msg.Nonce, orchaddr)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "couldn't check for existing valset confirm")
	}
	if found {
		return nil, sdkerrors.Wrap(types.ErrDuplicate, "signature duplicate")
	}

	// Persist signature
	key, err := k.SetValsetConfirm(ctx, *msg)
	if err != nil {
		// Should we include more details in the error?
		return nil, sdkerrors.Wrap(err, "couldn't set valset confirm")
	}

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

	// Verify the attestation is a data commitment
	at, found, err := k.GetAttestationByNonce(ctx, msg.Nonce)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "couldn't get attestation for nonce")
	}
	if !found {
		return nil, sdkerrors.Wrap(
			types.ErrNilAttestation,
			"confirm sent to a non existent attestation",
		)
	}
	if at.Type() != types.DataCommitmentRequestType {
		return nil, sdkerrors.Wrap(
			types.ErrAttestationNotDataCommitmentRequest,
			"confirm sent to an attestation that is not a data commitment request",
		)
	}

	// Verify the range is correct
	dcAt, ok := at.(*types.DataCommitment)
	if !ok {
		return nil, types.ErrAttestationNotCastToDataCommitment
	}
	if dcAt == nil {
		return nil, types.ErrNilDataCommitmentRequest
	}
	if dcAt.BeginBlock != msg.BeginBlock || dcAt.EndBlock != msg.EndBlock {
		return nil, types.ErrDataCommitmentConfirmWrongRange
	}

	// Decode the signature
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
	if !common.IsHexAddress(msg.EthAddress) {
		return nil, sdkerrors.Wrap(stakingtypes.ErrEthAddressNotHex, "ethereum address")
	}
	ethAddress := common.HexToAddress(msg.EthAddress)
	if validator.EthAddress != ethAddress.Hex() {
		return nil, sdkerrors.Wrap(types.ErrInvalid, "submitted eth address does not match delegate eth address")
	}

	// Verify if validator is part of the previous valset
	previousValset, err := k.GetLastValsetBeforeNonce(ctx, msg.Nonce)
	if err != nil {
		return nil, err
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
	err = types.ValidateEthereumSignature(hash.Bytes(), sigBytes, ethAddress)
	if err != nil {
		return nil, sdkerrors.Wrap(
			types.ErrInvalid,
			fmt.Sprintf(
				"signature verification failed expected sig by %s with checkpoint %s found %s",
				ethAddress.Hex(),
				msg.Commitment,
				msg.Signature,
			),
		)
	}

	// Check if the signature was already posted
	_, found, err = k.GetDataCommitmentConfirm(ctx, msg.EndBlock, msg.BeginBlock, validatorAddress)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "couldn't check for existing data commitment confirm")
	}
	if found {
		return nil, sdkerrors.Wrap(types.ErrDuplicate, "signature duplicate")
	}

	// Persist signature
	_, err = k.SetDataCommitmentConfirm(ctx, *msg)
	if err != nil {
		return nil, err
	}

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
