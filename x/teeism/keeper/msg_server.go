package keeper

import (
	"bytes"
	"context"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	"github.com/celestiaorg/celestia-app/v10/x/teeism/types"
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
// One attestation advances the state and authorizes its message batch together.
//
// A proof-carrying ISM has to split this in two, because two proofs project the
// same attestation through two different public-value shapes and each needs its
// own transaction. That split is also what forces the one-batch-per-state-root
// bookkeeping such a module needs, since the two halves can arrive out of step.
// Here there is one quote, one decode and one write, so none of that exists:
// replay is prevented by the state chain itself, because an attestation names the
// state it starts from and the transition rules require the height to advance.
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

	sdkCtx.GasMeter().ConsumeGas(uint64(len(update.MessageIDs))*types.DefaultMessageIDCost, "authorize messages")

	messages := make([]string, 0, len(update.MessageIDs))
	for _, messageId := range update.MessageIDs {
		if err := m.messages.Set(ctx, collections.Join(ism.Id.GetInternalId(), messageId[:])); err != nil {
			return nil, err
		}
		messages = append(messages, types.EncodeHex(messageId[:]))
	}

	ism.State = types.EncodeIsmState(update.NewState)
	if err := m.isms.Set(ctx, ism.Id.GetInternalId(), ism); err != nil {
		return nil, err
	}

	if err := EmitSubmitAttestationEvent(sdkCtx, ism, update.NewState, update.MessageIDs); err != nil {
		return nil, err
	}

	return &types.MsgSubmitAttestationResponse{
		State:     types.EncodeHex(ism.State),
		StateRoot: types.EncodeHex(update.NewState.StateRoot[:]),
		Messages:  messages,
	}, nil
}
