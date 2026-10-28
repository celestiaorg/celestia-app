package keeper

import (
	"bytes"
	"context"

	errorsmod "cosmossdk.io/errors"
	"github.com/celestiaorg/celestia-app/v11/x/zkism/types"
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

// CreateInterchainSecurityModule implements types.MsgServer.
func (m msgServer) CreateInterchainSecurityModule(ctx context.Context, msg *types.MsgCreateInterchainSecurityModule) (*types.MsgCreateInterchainSecurityModuleResponse, error) {
	// Deserializing the verifying key decompresses elliptic curve points, so charge for it first.
	sdk.UnwrapSDKContext(ctx).GasMeter().ConsumeGas(types.DefaultProofVerifyCostGroth16, "groth16 verifying key parse")
	if _, err := types.NewSP1Groth16Verifier(msg.Groth16Vkey); err != nil {
		return nil, err
	}

	ismId, err := m.coreKeeper.IsmRouter().GetNextSequence(ctx, types.ModuleTypeZkISM)
	if err != nil {
		return nil, err
	}

	newIsm := types.InterchainSecurityModule{
		Id:                  ismId,
		Owner:               msg.Creator,
		State:               msg.State,
		Groth16Vkey:         msg.Groth16Vkey,
		MerkleTreeAddress:   msg.MerkleTreeAddress,
		StateTransitionVkey: msg.StateTransitionVkey,
		StateMembershipVkey: msg.StateMembershipVkey,
	}

	if err := m.isms.Set(ctx, ismId.GetInternalId(), newIsm); err != nil {
		return nil, err
	}

	if err := EmitCreateISMEvent(sdk.UnwrapSDKContext(ctx), newIsm); err != nil {
		return nil, err
	}

	return &types.MsgCreateInterchainSecurityModuleResponse{
		Id: ismId,
	}, nil
}

// UpdateInterchainSecurityModule implements types.MsgServer.
func (m msgServer) UpdateInterchainSecurityModule(ctx context.Context, msg *types.MsgUpdateInterchainSecurityModule) (*types.MsgUpdateInterchainSecurityModuleResponse, error) {
	ism, err := m.isms.Get(ctx, msg.Id.GetInternalId())
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrIsmNotFound, "failed to get ism: %s", msg.Id.String())
	}

	var publicValues types.StateTransitionValues
	if err := publicValues.Unmarshal(msg.PublicValues); err != nil {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidType, err.Error())
	}

	if len(publicValues.State) < 32 || len(publicValues.NewState) < 32 {
		return nil, errorsmod.Wrapf(types.ErrInvalidTrustedState, "state must be at least 32 bytes")
	}

	if !bytes.Equal(ism.State, publicValues.State) {
		return nil, errorsmod.Wrapf(types.ErrInvalidTrustedState, "expected %x, got %x", ism.State, publicValues.State)
	}

	verifier, err := types.NewSP1Groth16Verifier(ism.Groth16Vkey)
	if err != nil {
		return nil, err
	}

	sdk.UnwrapSDKContext(ctx).GasMeter().ConsumeGas(types.DefaultProofVerifyCostGroth16, "groth16 verify")
	if err := verifier.VerifyProof(msg.Proof, ism.StateTransitionVkey, msg.PublicValues); err != nil {
		return nil, err
	}

	// Check if the state root has changed (first 32 bytes of state)
	oldStateRoot := ism.State[:32]
	newStateRoot := publicValues.NewState[:32]
	stateRootChanged := !bytes.Equal(oldStateRoot, newStateRoot)

	// Store the new State from outputs as the ISM state
	ism.State = publicValues.NewState
	if err := m.isms.Set(ctx, ism.Id.GetInternalId(), ism); err != nil {
		return nil, err
	}

	// Reset the message proof submitted flag only if the state root has changed
	if stateRootChanged {
		if err := m.submissions.Set(ctx, ism.Id.GetInternalId(), false); err != nil {
			return nil, err
		}
	}

	if err := EmitUpdateISMEvent(sdk.UnwrapSDKContext(ctx), ism); err != nil {
		return nil, err
	}

	return &types.MsgUpdateInterchainSecurityModuleResponse{
		State: ism.State,
	}, nil
}

// SubmitMessages implements types.MsgServer.
func (m msgServer) SubmitMessages(ctx context.Context, msg *types.MsgSubmitMessages) (*types.MsgSubmitMessagesResponse, error) {
	ism, err := m.isms.Get(ctx, msg.Id.GetInternalId())
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrIsmNotFound, "failed to get ism: %s", msg.Id.String())
	}

	var publicValues types.StateMembershipValues
	if err := publicValues.Unmarshal(msg.PublicValues); err != nil {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidType, err.Error())
	}

	if !bytes.Equal(publicValues.StateRoot[:], ism.State[:32]) {
		return nil, errorsmod.Wrapf(types.ErrInvalidStateRoot, "expected %x, got %x", ism.State[:32], publicValues.StateRoot)
	}

	// Check if a message proof has already been submitted for the current state root
	submitted, err := m.submissions.Get(ctx, ism.Id.GetInternalId())
	if err == nil && submitted {
		return nil, types.ErrMessageProofAlreadySubmitted
	}

	if !bytes.Equal(publicValues.MerkleTreeAddress[:], ism.MerkleTreeAddress) {
		return nil, errorsmod.Wrapf(types.ErrInvalidMerkleTreeAddress, "expected %x, got %x", ism.MerkleTreeAddress, publicValues.MerkleTreeAddress)
	}

	verifier, err := types.NewSP1Groth16Verifier(ism.Groth16Vkey)
	if err != nil {
		return nil, err
	}

	sdk.UnwrapSDKContext(ctx).GasMeter().ConsumeGas(types.DefaultProofVerifyCostGroth16, "groth16 verify")
	if err := verifier.VerifyProof(msg.Proof, ism.StateMembershipVkey, msg.PublicValues); err != nil {
		return nil, err
	}

	treeState := types.MerkleTreeState{
		Root:  publicValues.TreeRoot[:],
		Count: publicValues.Count,
	}
	if err := m.treeStates.Set(ctx, ism.Id.GetInternalId(), treeState); err != nil {
		return nil, err
	}

	// Mark that a message proof has been submitted for the current state root
	if err := m.submissions.Set(ctx, ism.Id.GetInternalId(), true); err != nil {
		return nil, err
	}

	if err := EmitSubmitMessagesEvent(sdk.UnwrapSDKContext(ctx), ism, treeState); err != nil {
		return nil, err
	}

	return &types.MsgSubmitMessagesResponse{
		StateRoot: types.EncodeHex(publicValues.StateRoot[:]),
		TreeRoot:  types.EncodeHex(treeState.Root),
		Count:     treeState.Count,
	}, nil
}

// UpdateVerifyingKeys implements types.MsgServer.
func (m msgServer) UpdateVerifyingKeys(ctx context.Context, msg *types.MsgUpdateVerifyingKeys) (*types.MsgUpdateVerifyingKeysResponse, error) {
	ism, err := m.isms.Get(ctx, msg.Id.GetInternalId())
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrIsmNotFound, "failed to get ism: %s", msg.Id.String())
	}

	if ism.Owner == "" || msg.Owner != ism.Owner {
		return nil, errorsmod.Wrapf(sdkerrors.ErrUnauthorized, "expected owner %s, got %s", ism.Owner, msg.Owner)
	}

	// Deserializing the verifying key decompresses elliptic curve points, so charge for it first.
	sdk.UnwrapSDKContext(ctx).GasMeter().ConsumeGas(types.DefaultProofVerifyCostGroth16, "groth16 verifying key parse")
	if _, err := types.NewSP1Groth16Verifier(msg.Groth16Vkey); err != nil {
		return nil, err
	}

	ism.Groth16Vkey = msg.Groth16Vkey
	ism.StateTransitionVkey = msg.StateTransitionVkey
	ism.StateMembershipVkey = msg.StateMembershipVkey
	if err := m.isms.Set(ctx, ism.Id.GetInternalId(), ism); err != nil {
		return nil, err
	}

	// Nothing proven under the replaced keys stays in force: drop the merkle tree state so no message verifies until
	// the new membership program has proven a tree root, and allow that proof for the current state root.
	if err := m.treeStates.Remove(ctx, ism.Id.GetInternalId()); err != nil {
		return nil, err
	}

	if err := m.submissions.Set(ctx, ism.Id.GetInternalId(), false); err != nil {
		return nil, err
	}

	if err := EmitUpdateVerifyingKeysEvent(sdk.UnwrapSDKContext(ctx), ism); err != nil {
		return nil, err
	}

	return &types.MsgUpdateVerifyingKeysResponse{}, nil
}

// TransferOwnership implements types.MsgServer. Only the current owner can transfer ownership to another address or
// renounce it. Renouncing is irreversible: with no owner, no account can call UpdateVerifyingKeys or TransferOwnership.
func (m msgServer) TransferOwnership(ctx context.Context, msg *types.MsgTransferOwnership) (*types.MsgTransferOwnershipResponse, error) {
	ism, err := m.isms.Get(ctx, msg.Id.GetInternalId())
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrIsmNotFound, "failed to get ism: %s", msg.Id.String())
	}

	if ism.Owner == "" || msg.Owner != ism.Owner {
		return nil, errorsmod.Wrapf(sdkerrors.ErrUnauthorized, "expected owner %s, got %s", ism.Owner, msg.Owner)
	}

	// ValidateBasic guarantees exactly one of new_owner and renounce_ownership is set and that new_owner is a valid
	// bech32 address. Store the canonical spelling: bech32 is case-insensitive, but the owner checks compare strings,
	// and wallets sign with the lowercase form.
	if msg.RenounceOwnership {
		ism.Owner = ""
	} else {
		newOwner, err := sdk.AccAddressFromBech32(msg.NewOwner)
		if err != nil {
			return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidAddress, "invalid new owner: %s", err)
		}
		ism.Owner = newOwner.String()
	}

	if err := m.isms.Set(ctx, ism.Id.GetInternalId(), ism); err != nil {
		return nil, err
	}

	if err := EmitTransferOwnershipEvent(sdk.UnwrapSDKContext(ctx), ism.Id, msg); err != nil {
		return nil, err
	}

	return &types.MsgTransferOwnershipResponse{}, nil
}
