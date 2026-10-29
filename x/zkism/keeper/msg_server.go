package keeper

import (
	"bytes"
	"context"
	"errors"

	"cosmossdk.io/collections"
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
		RootHistorySize:     msg.RootHistorySize,
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

	// Accept at most one transition proof per ISM per block. Skipped in simulation, which runs at the previous height.
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	height := sdkCtx.BlockHeight()
	if ism.StateHeight == height && sdkCtx.ExecMode() != sdk.ExecModeSimulate {
		return nil, errorsmod.Wrapf(types.ErrAlreadyUpdatedInBlock, "state updated at height %d", height)
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
	ism.StateHeight = height
	if err := m.isms.Set(ctx, ism.Id.GetInternalId(), ism); err != nil {
		return nil, err
	}

	// Keep the replaced state root for in-flight membership proofs.
	if stateRootChanged {
		entry := types.RootHistoryEntry{Root: oldStateRoot}
		if err := pushHistory(ctx, m.stateRootHistory, ism.Id.GetInternalId(), types.EffectiveRootHistorySize(ism.RootHistorySize), entry); err != nil {
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

	ismID := ism.Id.GetInternalId()

	// Accept at most one membership proof per ISM per block, and only if it adds leaves, so duplicate and stale
	// proofs are rejected. The per-block check is skipped in simulation.
	latest, err := m.treeStates.Get(ctx, ismID)
	hasLatest := err == nil
	if err != nil && !errors.Is(err, collections.ErrNotFound) {
		return nil, err
	}
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	height := sdkCtx.BlockHeight()
	if hasLatest && latest.Height == height && sdkCtx.ExecMode() != sdk.ExecModeSimulate {
		return nil, errorsmod.Wrapf(types.ErrAlreadyUpdatedInBlock, "merkle tree state proven at height %d", height)
	}
	if hasLatest && publicValues.Count <= latest.Count {
		return nil, errorsmod.Wrapf(types.ErrStaleMerkleTreeState, "count %d does not exceed latest proven count %d", publicValues.Count, latest.Count)
	}

	if !bytes.Equal(publicValues.StateRoot[:], ism.State[:32]) {
		known, err := historyContains(ctx, m.stateRootHistory, ismID, func(entry types.RootHistoryEntry) bool {
			return bytes.Equal(entry.Root, publicValues.StateRoot[:])
		})
		if err != nil {
			return nil, err
		}
		if !known {
			return nil, errorsmod.Wrapf(types.ErrInvalidStateRoot, "expected %x or a state root in the root history, got %x", ism.State[:32], publicValues.StateRoot)
		}
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

	// Keep the replaced tree state for in-flight relayed messages.
	if hasLatest {
		entry := types.RootHistoryEntry{Root: latest.Root, Count: latest.Count}
		if err := pushHistory(ctx, m.treeStateHistory, ismID, types.EffectiveRootHistorySize(ism.RootHistorySize), entry); err != nil {
			return nil, err
		}
	}

	treeState := types.MerkleTreeState{
		Root:   publicValues.TreeRoot[:],
		Count:  publicValues.Count,
		Height: height,
	}
	if err := m.treeStates.Set(ctx, ismID, treeState); err != nil {
		return nil, err
	}

	if err := EmitSubmitMessagesEvent(sdk.UnwrapSDKContext(ctx), ism.Id, publicValues.StateRoot[:], treeState); err != nil {
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

	// Drop everything proven under the old keys; no message verifies until the new program submits.
	if err := m.treeStates.Remove(ctx, ism.Id.GetInternalId()); err != nil {
		return nil, err
	}

	if err := clearHistory(ctx, m.treeStateHistory, ism.Id.GetInternalId()); err != nil {
		return nil, err
	}

	if err := clearHistory(ctx, m.stateRootHistory, ism.Id.GetInternalId()); err != nil {
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

// UpdateRootHistorySize implements types.MsgServer. Only the owner can call it; shrinking drops the oldest entries.
func (m msgServer) UpdateRootHistorySize(ctx context.Context, msg *types.MsgUpdateRootHistorySize) (*types.MsgUpdateRootHistorySizeResponse, error) {
	ism, err := m.isms.Get(ctx, msg.Id.GetInternalId())
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrIsmNotFound, "failed to get ism: %s", msg.Id.String())
	}

	if ism.Owner == "" || msg.Owner != ism.Owner {
		return nil, errorsmod.Wrapf(sdkerrors.ErrUnauthorized, "expected owner %s, got %s", ism.Owner, msg.Owner)
	}

	if err := types.ValidateRootHistorySize(msg.RootHistorySize); err != nil {
		return nil, errorsmod.Wrap(types.ErrInvalidRootHistorySize, err.Error())
	}

	ism.RootHistorySize = msg.RootHistorySize
	if err := m.isms.Set(ctx, ism.Id.GetInternalId(), ism); err != nil {
		return nil, err
	}

	size := types.EffectiveRootHistorySize(ism.RootHistorySize)
	if err := resizeHistory(ctx, m.stateRootHistory, ism.Id.GetInternalId(), size); err != nil {
		return nil, err
	}

	if err := resizeHistory(ctx, m.treeStateHistory, ism.Id.GetInternalId(), size); err != nil {
		return nil, err
	}

	if err := EmitUpdateRootHistorySizeEvent(sdk.UnwrapSDKContext(ctx), ism); err != nil {
		return nil, err
	}

	return &types.MsgUpdateRootHistorySizeResponse{}, nil
}
