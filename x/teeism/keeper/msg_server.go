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
// checkpoint and the set of enclave identities, all of which are public and
// auditable at creation time, so an ISM nobody trusts is simply an ISM nobody
// routes to.
func (m msgServer) CreateInterchainSecurityModule(ctx context.Context, msg *types.MsgCreateInterchainSecurityModule) (*types.MsgCreateInterchainSecurityModuleResponse, error) {
	if err := msg.ValidateBasic(); err != nil {
		return nil, err
	}
	// ValidateBasic checked the set against the initial state; this stores it in
	// canonical order.
	identities, err := types.SortIdentities(msg.Identities)
	if err != nil {
		return nil, err
	}
	ismId, err := m.coreKeeper.IsmRouter().GetNextSequence(ctx, types.ModuleTypeTeeISM)
	if err != nil {
		return nil, err
	}

	newIsm := types.InterchainSecurityModule{
		Id:                ismId,
		Owner:             msg.Creator,
		State:             msg.State,
		MerkleTreeAddress: msg.MerkleTreeAddress,
		Identities:        identities,
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
	if err := msg.ValidateBasic(); err != nil {
		return nil, err
	}
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	ism, err := m.isms.Get(ctx, msg.Id.GetInternalId())
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrIsmNotFound, "failed to get ism: %s", msg.Id.String())
	}
	pinned, err := ism.IdentityDigest()
	if err != nil {
		return nil, errorsmod.Wrapf(err, "ism %s", ism.Id.String())
	}

	// Meter before verifying, so a submission that fails verification still pays
	// for the work it caused.
	sdkCtx.GasMeter().ConsumeGas(types.DefaultAttestationVerifyCost, "dcap attestation verify")

	result, err := types.VerifyAttestation(
		msg.Quote,
		msg.EventLog,
		msg.Collateral,
		msg.Payload,
		ism.Identities,
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

	// The enclave copies the identity digest forward from the state it was handed.
	// Checking the new state still names this ISM's set of enclaves stops an
	// attestation from quietly re-pointing the ISM at a different set.
	if update.NewState.IdentityDigest != pinned {
		return nil, types.ErrIdentityMismatch.Wrap("attested state names different enclaves than this ism pins")
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

// SetEnclaveIdentities implements types.MsgServer.
//
// The owner's control over which enclaves advance an ISM: for when the enclave's
// platform changes under it (a new OS image, new firmware), an enclave is added for
// redundancy, or one is retired. The whole set is replaced at once, so a rotation
// is: pin {old, new}, move the relayer, pin {new}, and no attestation is refused in
// between. The trusted state keeps its root, height and light client store; only
// the identity digest it carries is swapped for the new set digest, which is what
// the enclave copies forward into every state it attests. So nothing in flight is
// lost, and no new ISM or router change is needed.
func (m msgServer) SetEnclaveIdentities(ctx context.Context, msg *types.MsgSetEnclaveIdentities) (*types.MsgSetEnclaveIdentitiesResponse, error) {
	if err := msg.ValidateBasic(); err != nil {
		return nil, err
	}
	ism, err := m.isms.Get(ctx, msg.Id.GetInternalId())
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrIsmNotFound, "failed to get ism: %s", msg.Id.String())
	}
	previous, err := ism.SetIdentities(msg.Owner, msg.Identities)
	if err != nil {
		return nil, err
	}
	if err := m.isms.Set(ctx, ism.Id.GetInternalId(), ism); err != nil {
		return nil, err
	}

	if err := EmitSetEnclaveIdentitiesEvent(sdk.UnwrapSDKContext(ctx), ism, previous); err != nil {
		return nil, err
	}
	return &types.MsgSetEnclaveIdentitiesResponse{}, nil
}

// UpdateOwner implements types.MsgServer.
//
// Hands the ism, and with it every owner control (re-pinning, verifier
// overrides), to a new owner, typically a multisig.
func (m msgServer) UpdateOwner(ctx context.Context, msg *types.MsgUpdateOwner) (*types.MsgUpdateOwnerResponse, error) {
	if err := msg.ValidateBasic(); err != nil {
		return nil, err
	}
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
	if err := msg.ValidateBasic(); err != nil {
		return nil, err
	}
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
