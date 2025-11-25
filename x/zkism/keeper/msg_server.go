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

// CreateConsensusISM implements types.MsgServer.
func (m msgServer) CreateConsensusISM(ctx context.Context, msg *types.MsgCreateConsensusISM) (*types.MsgCreateConsensusISMResponse, error) {
	ismId, err := m.coreKeeper.IsmRouter().GetNextSequence(ctx, types.InterchainSecurityModuleTypeStateTransition)
	if err != nil {
		return nil, errorsmod.Wrap(err, err.Error())
	}

	newIsm := types.ConsensusISM{
		Id:                  ismId,
		Owner:               msg.Creator,
		TrustedState:        msg.TrustedState,
		Groth16Vkey:         msg.Groth16Vkey,
		StateTransitionVkey: msg.StateTransitionVkey,
	}

	if err := m.isms.Set(ctx, ismId.GetInternalId(), &newIsm); err != nil {
		return nil, errorsmod.Wrap(err, err.Error())
	}

	return &types.MsgCreateConsensusISMResponse{
		TrustedState: msg.TrustedState,
	}, nil
}

// UpdateConsensusISM implements types.MsgServer.
func (m msgServer) UpdateConsensusISM(ctx context.Context, msg *types.MsgUpdateConsensusISM) (*types.MsgUpdateConsensusISMResponse, error) {
	ismInterface, err := m.isms.Get(ctx, msg.Id.GetInternalId())
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrIsmNotFound, "failed to get ism: %s", msg.Id)
	}

	ism, ok := ismInterface.(*types.ConsensusISM)
	if !ok {
		return nil, errorsmod.Wrapf(types.ErrIsmNotFound, "ISM is not a ConsensusISM: %s", msg.Id)
	}

	var publicValues types.StateTransitionPublicValues
	if err := publicValues.Unmarshal(msg.PublicValues); err != nil {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidType, err.Error())
	}

	if err := ism.ValidatePublicValues(ctx, publicValues); err != nil {
		return nil, err
	}

	verifier, err := types.NewSP1Groth16Verifier(ism.Groth16Vkey)
	if err != nil {
		return nil, err
	}

	if err := verifier.VerifyProof(msg.Proof, ism.StateTransitionVkey, msg.PublicValues); err != nil {
		return nil, err
	}

	// extract the new trusted state from trusted state
	ism.TrustedState = publicValues.NewTrustedState
	if err := m.isms.Set(ctx, ism.Id.GetInternalId(), ism); err != nil {
		return nil, err
	}

	if err := EmitUpdateConsensusISMEvent(sdk.UnwrapSDKContext(ctx), *ism); err != nil {
		return nil, err
	}

	return &types.MsgUpdateConsensusISMResponse{
		TrustedState: ism.TrustedState,
	}, nil
}

// CreateEvolveEvmISM implements types.MsgServer.
func (m msgServer) CreateEvolveEvmISM(ctx context.Context, msg *types.MsgCreateEvolveEvmISM) (*types.MsgCreateEvolveEvmISMResponse, error) {
	ismId, err := m.coreKeeper.IsmRouter().GetNextSequence(ctx, types.InterchainSecurityModuleTypeZKExecution)
	if err != nil {
		return nil, errorsmod.Wrap(err, err.Error())
	}

	newIsm := types.EvolveEvmISM{
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

	if err := m.isms.Set(ctx, ismId.GetInternalId(), &newIsm); err != nil {
		return nil, errorsmod.Wrap(err, err.Error())
	}

	if err := EmitCreateISMEvent(sdk.UnwrapSDKContext(ctx), newIsm); err != nil {
		return nil, err
	}

	return &types.MsgCreateEvolveEvmISMResponse{
		Id: ismId,
	}, nil
}

// UpdateEvolveEvmISM implements types.MsgServer.
func (m msgServer) UpdateEvolveEvmISM(ctx context.Context, msg *types.MsgUpdateEvolveEvmISM) (*types.MsgUpdateEvolveEvmISMResponse, error) {
	ismInterface, err := m.isms.Get(ctx, msg.Id.GetInternalId())
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrIsmNotFound, "failed to get ism: %s", msg.Id.String())
	}

	ism, ok := ismInterface.(*types.EvolveEvmISM)
	if !ok {
		return nil, errorsmod.Wrapf(types.ErrIsmNotFound, "ISM is not a EvolveEvmISM: %s", msg.Id.String())
	}

	var publicValues types.EvExecutionPublicValues
	if err := publicValues.Unmarshal(msg.PublicValues); err != nil {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidType, err.Error())
	}

	if err := ism.ValidatePublicValues(ctx, publicValues, m.Keeper); err != nil {
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

	if err := EmitUpdateISMEvent(sdk.UnwrapSDKContext(ctx), *ism); err != nil {
		return nil, err
	}

	return &types.MsgUpdateEvolveEvmISMResponse{
		Height:             ism.Height,
		StateRoot:          hex.EncodeToString(ism.StateRoot),
		CelestiaHeaderHash: hex.EncodeToString(ism.CelestiaHeaderHash),
		CelestiaHeight:     ism.CelestiaHeight,
	}, nil
}

// SubmitMessages implements types.MsgServer.
func (m msgServer) SubmitMessages(ctx context.Context, msg *types.MsgSubmitMessages) (*types.MsgSubmitMessagesResponse, error) {
	ismInterface, err := m.isms.Get(ctx, msg.Id.GetInternalId())
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrIsmNotFound, "failed to get ism: %s", msg.Id.String())
	}

	// todo: add an implementation for the ConsensusISM, where the state_root is derived from the trusted_state
	ism, ok := ismInterface.(*types.EvolveEvmISM)
	if !ok {
		return nil, errorsmod.Wrapf(types.ErrIsmNotFound, "ISM is not a EvolveEvmISM: %s", msg.Id.String())
	}

	var publicValues types.EvHyperlanePublicValues
	if err := publicValues.Unmarshal(msg.PublicValues); err != nil {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidType, err.Error())
	}

	// this must be handled differently for the ConsensusISM, where the state_root should be the first 32 bytes of trusted_state
	// todo: add an implementation for that case with the only difference being the state_root comparison, all other logic is shared
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
