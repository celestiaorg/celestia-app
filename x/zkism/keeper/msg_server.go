package keeper

import (
	"bytes"
	"context"
	"encoding/hex"

	errorsmod "cosmossdk.io/errors"
	"github.com/celestiaorg/celestia-app/v6/x/zkism/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

var _ types.MsgServer = msgServer{}

type msgServer struct {
	*Keeper
}

// NewMsgServerImpl creates and returns a new module MsgServer instance.
func NewMsgServerImpl(keeper *Keeper) types.MsgServer {
	return &msgServer{keeper}
}

// CreateStateTransitionVerifier implements types.MsgServer.
func (m msgServer) CreateStateTransitionVerifier(ctx context.Context, msg *types.MsgCreateStateTransitionVerifier) (*types.MsgCreateStateTransitionVerifierResponse, error) {
	// TODO: Implement CreateStateTransitionVerifier
	verifierId, err := m.coreKeeper.IsmRouter().GetNextSequence(ctx, types.InterchainSecurityModuleTypeZKExecution)
	if err != nil {
		return nil, errorsmod.Wrap(err, err.Error())
	}

	newVerifier := types.StateTransitionVerifier{
		Id:                  verifierId,
		Owner:               msg.Creator,
		TrustedState:        msg.TrustedState,
		Groth16Vkey:         msg.Groth16Vkey,
		StateTransitionVkey: msg.StateTransitionVkey,
	}

	if err := m.verifiers.Set(ctx, verifierId.GetInternalId(), newVerifier); err != nil {
		return nil, errorsmod.Wrap(err, err.Error())
	}

	return &types.MsgCreateStateTransitionVerifierResponse{
		TrustedState: msg.TrustedState,
	}, nil
}

// UpdateStateTranstionVerifier implements types.MsgServer.
func (m msgServer) UpdateStateTransitionVerifier(ctx context.Context, msg *types.MsgUpdateStateTransitionVerifier) (*types.MsgUpdateStateTransitionVerifierResponse, error) {
	// panic immediately
	return nil, errorsmod.Wrapf(types.ErrIsmNotFound, "failed to get verifier: %s", msg.Id)

	vrf, err := m.verifiers.Get(ctx, msg.Id.GetInternalId())
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrIsmNotFound, "failed to get verifier: %s", msg.Id)
	}

	var publicValues types.StateTransitionPublicValues
	if err := publicValues.Unmarshal(msg.PublicValues); err != nil {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidType, err.Error())
	}

	if err := m.validateGenericPublicValues(ctx, vrf, publicValues); err != nil {
		return nil, err
	}

	verifier, err := types.NewSP1Groth16Verifier(vrf.Groth16Vkey)
	if err != nil {
		return nil, err
	}

	if err := verifier.VerifyProof(msg.Proof, vrf.StateTransitionVkey, msg.PublicValues); err != nil {
		return nil, err
	}

	// extract the new trusted state from trusted state
	vrf.TrustedState = publicValues.NewTrustedState[:]
	if err := m.verifiers.Set(ctx, vrf.Id.GetInternalId(), vrf); err != nil {
		return nil, err
	}

	if err := EmitUpdateStateTransitionVerifierEvent(sdk.UnwrapSDKContext(ctx), vrf); err != nil {
		return nil, err
	}

	return &types.MsgUpdateStateTransitionVerifierResponse{
		TrustedState: vrf.TrustedState,
	}, nil
}

// CreateZKExecutionISM implements types.MsgServer.
func (m msgServer) CreateZKExecutionISM(ctx context.Context, msg *types.MsgCreateZKExecutionISM) (*types.MsgCreateZKExecutionISMResponse, error) {
	ismId, err := m.coreKeeper.IsmRouter().GetNextSequence(ctx, types.InterchainSecurityModuleTypeZKExecution)
	if err != nil {
		return nil, errorsmod.Wrap(err, err.Error())
	}

	newIsm := types.ZKExecutionISM{
		Id:                  ismId,
		Owner:               msg.Creator,
		StateRoot:           msg.StateRoot,
		Height:              msg.Height,
		CelestiaHeaderHash:  msg.CelestiaHeaderHash,
		CelestiaHeight:      msg.CelestiaHeight,
		Namespace:           msg.Namespace,
		SequencerPublicKey:  msg.SequencerPublicKey,
		Groth16Vkey:         msg.Groth16Vkey,
		StateTransitionVkey: msg.StateTransitionVkey,
		StateMembershipVkey: msg.StateMembershipVkey,
	}

	if err := m.isms.Set(ctx, ismId.GetInternalId(), newIsm); err != nil {
		return nil, errorsmod.Wrap(err, err.Error())
	}

	if err := EmitCreateISMEvent(sdk.UnwrapSDKContext(ctx), newIsm); err != nil {
		return nil, err
	}

	return &types.MsgCreateZKExecutionISMResponse{
		Id: ismId,
	}, nil
}

// UpdateZKExecutionISM implements types.MsgServer.
func (m msgServer) UpdateZKExecutionISM(ctx context.Context, msg *types.MsgUpdateZKExecutionISM) (*types.MsgUpdateZKExecutionISMResponse, error) {
	ism, err := m.isms.Get(ctx, msg.Id.GetInternalId())
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrIsmNotFound, "failed to get ism: %s", msg.Id.String())
	}

	var publicValues types.EvExecutionPublicValues
	if err := publicValues.Unmarshal(msg.PublicValues); err != nil {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidType, err.Error())
	}

	if err := m.validatePublicValues(ctx, ism, publicValues); err != nil {
		return nil, err
	}

	verifier, err := types.NewSP1Groth16Verifier(ism.Groth16Vkey)
	if err != nil {
		return nil, err
	}

	if err := verifier.VerifyProof(msg.Proof, ism.StateTransitionVkey, msg.PublicValues); err != nil {
		return nil, err
	}

	ism.Height = publicValues.NewHeight
	ism.StateRoot = publicValues.NewStateRoot[:]
	ism.CelestiaHeight = publicValues.CelestiaHeight
	ism.CelestiaHeaderHash = publicValues.CelestiaHeaderHash[:]
	if err := m.isms.Set(ctx, ism.Id.GetInternalId(), ism); err != nil {
		return nil, err
	}

	if err := EmitUpdateISMEvent(sdk.UnwrapSDKContext(ctx), ism); err != nil {
		return nil, err
	}

	return &types.MsgUpdateZKExecutionISMResponse{
		Height:             ism.Height,
		StateRoot:          hex.EncodeToString(ism.StateRoot),
		CelestiaHeaderHash: hex.EncodeToString(ism.CelestiaHeaderHash),
		CelestiaHeight:     ism.CelestiaHeight,
	}, nil
}

// SubmitMessages implements types.MsgServer.
func (m msgServer) SubmitMessages(ctx context.Context, msg *types.MsgSubmitMessages) (*types.MsgSubmitMessagesResponse, error) {
	ism, err := m.isms.Get(ctx, msg.Id.GetInternalId())
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrIsmNotFound, "failed to get ism: %s", msg.Id.String())
	}

	var publicValues types.EvHyperlanePublicValues
	if err := publicValues.Unmarshal(msg.PublicValues); err != nil {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidType, err.Error())
	}

	if !bytes.Equal(publicValues.StateRoot[:], ism.StateRoot) {
		return nil, errorsmod.Wrapf(types.ErrInvalidStateRoot, "expected %x, got %x", ism.StateRoot, publicValues.StateRoot)
	}

	verifier, err := types.NewSP1Groth16Verifier(ism.Groth16Vkey)
	if err != nil {
		return nil, err
	}

	if err := verifier.VerifyProof(msg.Proof, ism.StateMembershipVkey, msg.PublicValues); err != nil {
		return nil, err
	}

	for _, messageId := range publicValues.MessageIds {
		if err := m.messages.Set(ctx, messageId[:]); err != nil {
			return nil, err
		}
	}

	if err := EmitSubmitMessagesEvent(sdk.UnwrapSDKContext(ctx), ism.StateRoot, publicValues.MessageIds); err != nil {
		return nil, err
	}

	return &types.MsgSubmitMessagesResponse{}, nil
}

// UpdateParams implements types.MsgServer.
func (m msgServer) UpdateParams(ctx context.Context, msg *types.MsgUpdateParams) (*types.MsgUpdateParamsResponse, error) {
	if msg.Authority != m.authority {
		return nil, errorsmod.Wrapf(sdkerrors.ErrUnauthorized, "invalid authority; expected %s, got %s", m.authority, msg.Authority)
	}

	if err := m.params.Set(ctx, msg.Params); err != nil {
		return nil, err
	}

	return &types.MsgUpdateParamsResponse{}, nil
}
