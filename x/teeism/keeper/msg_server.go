package keeper

import (
	"bytes"
	"context"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	"github.com/celestiaorg/celestia-app/v11/x/teeism/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
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
//
// Creation is permissionless. The initial state pins the origin light client
// checkpoint and the enclave identity, both of which are public and auditable at
// creation time, so an ISM nobody trusts is simply an ISM nobody routes to.
func (m msgServer) CreateInterchainSecurityModule(ctx context.Context, msg *types.MsgCreateInterchainSecurityModule) (*types.MsgCreateInterchainSecurityModuleResponse, error) {
	ismId, err := m.coreKeeper.IsmRouter().GetNextSequence(ctx, types.ModuleTypeTeeISM)
	if err != nil {
		return nil, err
	}

	newIsm := types.InterchainSecurityModule{
		Id:                ismId,
		Owner:             msg.Creator,
		State:             msg.State,
		MerkleTreeAddress: msg.MerkleTreeAddress,
		Identity:          msg.Identity,
	}

	if err := m.isms.Set(ctx, ismId.GetInternalId(), newIsm); err != nil {
		return nil, err
	}

	if err := EmitCreateISMEvent(sdk.UnwrapSDKContext(ctx), newIsm); err != nil {
		return nil, err
	}

	return &types.MsgCreateInterchainSecurityModuleResponse{Id: ismId}, nil
}

// SubmitAttestation implements types.MsgServer.
//
// One attestation advances the state and records the origin tree root it vouches
// for. Messages are delivered separately, each proving its place under an attested
// root with a merkle branch in its metadata (Keeper.Verify), so nothing here grows
// with how much the origin dispatched.
//
// Replay is prevented by the state chain itself: an attestation names the state it
// starts from and the transition rules require the height to advance.
func (m msgServer) SubmitAttestation(ctx context.Context, msg *types.MsgSubmitAttestation) (*types.MsgSubmitAttestationResponse, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	ism, err := m.isms.Get(ctx, msg.Id.GetInternalId())
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrIsmNotFound, "failed to get ism: %s", msg.Id.String())
	}
	if ism.Identity == nil {
		return nil, types.ErrInvalidEnclaveIdentity.Wrapf("ism %s has no pinned enclave identity", ism.Id.String())
	}

	// Meter before verifying, so a submission that fails verification still pays
	// for the work it caused.
	sdkCtx.GasMeter().ConsumeGas(types.DefaultAttestationVerifyCost, "dcap attestation verify")

	result, err := types.VerifyAttestation(
		msg.Quote,
		msg.EventLog,
		msg.Collateral,
		msg.Payload,
		ism.Identity,
		sdkCtx.BlockTime(),
		ism.VerifierOverrides,
	)
	if err != nil {
		return nil, err
	}
	update := result.Update

	// The attestation must start from exactly the state this ISM holds. This is
	// what makes replay impossible: once the state advances, every earlier
	// attestation names a state that no longer exists.
	prev := types.EncodeIsmState(update.PrevState)
	if !bytes.Equal(ism.State, prev) {
		return nil, errorsmod.Wrapf(types.ErrInvalidTrustedState, "expected %x, got %x", ism.State, prev)
	}

	// The enclave pins its own identity into the state it produces. Checking the
	// new state still names this ISM's enclave stops an attestation from quietly
	// re-pointing the ISM at a different one.
	if update.NewState.IdentityDigest != ism.Identity.Digest() {
		return nil, types.ErrIdentityMismatch.Wrap("attested state names a different enclave than this ism pins")
	}

	if !bytes.Equal(update.MerkleTreeAddress[:], ism.MerkleTreeAddress) {
		return nil, errorsmod.Wrapf(types.ErrInvalidMerkleTreeAddress, "expected %x, got %x",
			ism.MerkleTreeAddress, update.MerkleTreeAddress[:])
	}

	// An attestation that brings the root the ism already holds carries no new
	// leaves; it may still move the state, but not more often than the cadence
	// allows, or anyone with the public enclave could keep invalidating the
	// relayer's attestations in flight. The first attestation always passes:
	// nothing is held yet.
	rootChanged := !bytes.Equal(update.TreeRoot[:], ism.TreeRoot)
	if err := types.VerifyBatchCadence(update.PrevState, update.NewState, rootChanged); err != nil {
		return nil, err
	}

	// Every root ever attested stays accepted for delivery, so a proof a relayer
	// built against one is never invalidated by a later attestation.
	if err := m.roots.Set(ctx, collections.Join(ism.Id.GetInternalId(), update.TreeRoot[:])); err != nil {
		return nil, err
	}

	ism.State = types.EncodeIsmState(update.NewState)
	ism.TreeRoot = update.TreeRoot[:]
	ism.TreeCount = update.TreeCount
	if err := m.isms.Set(ctx, ism.Id.GetInternalId(), ism); err != nil {
		return nil, err
	}

	if err := EmitSubmitAttestationEvent(sdkCtx, ism, update.NewState); err != nil {
		return nil, err
	}

	return &types.MsgSubmitAttestationResponse{
		State:     types.EncodeHex(ism.State),
		StateRoot: types.EncodeHex(update.NewState.StateRoot[:]),
		TreeRoot:  types.EncodeHex(ism.TreeRoot),
		TreeCount: ism.TreeCount,
	}, nil
}

// UpdateEnclaveIdentity implements types.MsgServer.
//
// The owner's emergency control over which enclave advances an ISM, for when the
// enclave's platform changes under it (a new OS image, new firmware) or the
// enclave has to be replaced. The trusted state keeps its root, height and light
// client store; only the identity digest it carries is swapped, which is what the
// enclave copies forward into every state it attests. So nothing in flight is
// lost, and no new ISM or router change is needed.
func (m msgServer) UpdateEnclaveIdentity(ctx context.Context, msg *types.MsgUpdateEnclaveIdentity) (*types.MsgUpdateEnclaveIdentityResponse, error) {
	ism, err := m.isms.Get(ctx, msg.Id.GetInternalId())
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrIsmNotFound, "failed to get ism: %s", msg.Id.String())
	}
	previous, err := ism.Repin(msg.Owner, msg.Identity)
	if err != nil {
		return nil, err
	}
	if err := m.isms.Set(ctx, ism.Id.GetInternalId(), ism); err != nil {
		return nil, err
	}

	if err := EmitUpdateEnclaveIdentityEvent(sdk.UnwrapSDKContext(ctx), ism, previous); err != nil {
		return nil, err
	}
	return &types.MsgUpdateEnclaveIdentityResponse{}, nil
}

// UpdateOwner implements types.MsgServer.
//
// Hands the ism, and with it every owner control (re-pinning, verifier
// overrides), to a new owner, typically a multisig.
func (m msgServer) UpdateOwner(ctx context.Context, msg *types.MsgUpdateOwner) (*types.MsgUpdateOwnerResponse, error) {
	ism, err := m.isms.Get(ctx, msg.Id.GetInternalId())
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrIsmNotFound, "failed to get ism: %s", msg.Id.String())
	}
	previous, err := ism.TransferOwnership(msg.Owner, msg.NewOwner)
	if err != nil {
		return nil, err
	}
	if err := m.isms.Set(ctx, ism.Id.GetInternalId(), ism); err != nil {
		return nil, err
	}

	if err := EmitUpdateOwnerEvent(sdk.UnwrapSDKContext(ctx), ism, previous); err != nil {
		return nil, err
	}
	return &types.MsgUpdateOwnerResponse{}, nil
}

// SetVerifierOverrides implements types.MsgServer.
//
// The owner's emergency control over Intel's side of verification, for when
// something Intel publishes or anchors breaks while the ism is live. Every change
// is an event, so an override is never silent.
func (m msgServer) SetVerifierOverrides(ctx context.Context, msg *types.MsgSetVerifierOverrides) (*types.MsgSetVerifierOverridesResponse, error) {
	ism, err := m.isms.Get(ctx, msg.Id.GetInternalId())
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrIsmNotFound, "failed to get ism: %s", msg.Id.String())
	}
	previous, err := ism.SetVerifierOverrides(msg.Owner, msg.Overrides)
	if err != nil {
		return nil, err
	}
	if err := m.isms.Set(ctx, ism.Id.GetInternalId(), ism); err != nil {
		return nil, err
	}

	if err := EmitSetVerifierOverridesEvent(sdk.UnwrapSDKContext(ctx), ism, previous); err != nil {
		return nil, err
	}
	return &types.MsgSetVerifierOverridesResponse{}, nil
}
