package groth16

import (
	"bytes"

	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/backend/groth16"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	clienttypes "github.com/cosmos/ibc-go/v6/modules/core/02-client/types"
	"github.com/cosmos/ibc-go/v6/modules/core/exported"
)

// CheckHeaderAndUpdateState checks if the provided header is valid, and if valid it will:
// create the consensus state for the header.Height
// and update the client state if the header height is greater than the latest client state height
// It returns an error if:
// - the client or header provided are not parseable to tendermint types
// - the header is invalid
// - header height is less than or equal to the trusted header height
// - header revision is not equal to trusted header revision
// - header valset commit verification fails
// - header timestamp is past the trusting period in relation to the consensus state
// - header timestamp is less than or equal to the consensus state timestamp
//
// UpdateClient may be used to either create a consensus state for:
// - a future height greater than the latest client state height
// - a past height that was skipped during bisection
// If we are updating to a past height, a consensus state is created for that height to be persisted in client store
// If we are updating to a future height, the consensus state is created and the client state is updated to reflect
// the new latest height
// UpdateClient must only be used to update within a single revision, thus header revision number and trusted height's revision
// number must be the same. To update to a new revision, use a separate upgrade path
// Tendermint client validity checking uses the bisection algorithm described
// in the [Tendermint spec](https://github.com/tendermint/spec/blob/master/spec/consensus/light-client.md).
//
// Misbehaviour Detection:
// UpdateClient will detect implicit misbehaviour by enforcing certain invariants on any new update call and will return a frozen client.
// 1. Any valid update that creates a different consensus state for an already existing height is evidence of misbehaviour and will freeze client.
// 2. Any valid update that breaks time monotonicity with respect to its neighboring consensus states is evidence of misbehaviour and will freeze client.
// Misbehaviour sets frozen height to {0, 1} since it is only used as a boolean value (zero or non-zero).
//
// Pruning:
// UpdateClient will additionally retrieve the earliest consensus state for this clientID and check if it is expired. If it is,
// that consensus state will be pruned from store along with all associated metadata. This will prevent the client store from
// becoming bloated with expired consensus states that can no longer be used for updates and packet verification.
func (cs ClientState) CheckHeaderAndUpdateState(
	ctx sdk.Context, cdc codec.BinaryCodec, clientStore sdk.KVStore,
	header exported.Header,
) (exported.ClientState, exported.ConsensusState, error) {
	h, ok := header.(*Header)
	if !ok {
		return nil, nil, sdkerrors.Wrapf(
			clienttypes.ErrInvalidHeader, "expected type %T, got %T", &Header{}, header,
		)
	}

	// Check if the Client store already has a consensus state for the header's height
	// If the consensus state exists, and it matches the header then we return early
	// since header has already been submitted in a previous UpdateClient.
	prevConsState, _ := GetConsensusState(clientStore, cdc, header.GetHeight())
	if prevConsState != nil {
		return &cs, prevConsState, nil
	}

	// get consensus state from clientStore
	trustedConsState, err := GetConsensusState(clientStore, cdc, h.GetTrustedHeight())
	if err != nil {
		return nil, nil, sdkerrors.Wrapf(
			err, "could not get consensus state from clientstore at TrustedHeight: %s", h.TrustedHeight,
		)
	}

	vk, err := cs.GetStateTransitionVerifierKey()
	if err != nil {
		return nil, nil, err
	}

	witness, err := h.GenerateStateTransitionPublicWitness(trustedConsState.StateRoot)
	if err != nil {
		return nil, nil, err
	}

	proof := groth16.NewProof(ecc.BN254)
	_, err = proof.ReadFrom(bytes.NewReader(h.StateTransitionProof))
	if err != nil {
		return nil, nil, err
	}

	err = groth16.Verify(proof, vk, witness)
	if err != nil {
		return nil, nil, err
	}

	// Check the earliest consensus state to see if it is expired, if so then set the prune height
	// so that we can delete consensus state and all associated metadata.
	var (
		pruneHeight exported.Height
		pruneError  error
	)
	pruneCb := func(height exported.Height) bool {
		consState, err := GetConsensusState(clientStore, cdc, height)
		// this error should never occur
		if err != nil {
			pruneError = err
			return true
		}
		if consState.IsExpired(ctx.BlockTime()) {
			pruneHeight = height
		}
		return true
	}
	err = IterateConsensusStateAscending(clientStore, pruneCb)
	if err != nil {
		return nil, nil, err
	}
	if pruneError != nil {
		return nil, nil, pruneError
	}
	// if pruneHeight is set, delete consensus state and metadata
	if pruneHeight != nil {
		deleteConsensusState(clientStore, pruneHeight)
		deleteConsensusMetadata(clientStore, pruneHeight)
	}

	newClientState, consensusState := update(ctx, clientStore, &cs, h)
	return newClientState, consensusState, nil
}

// update the consensus state from a new header and set processed time metadata
func update(ctx sdk.Context, clientStore sdk.KVStore, clientState *ClientState, header *Header) (*ClientState, *ConsensusState) {
	consensusState := &ConsensusState{
		Timestamp: ctx.BlockTime(), // this should really be the Celestia block time at the newHeight
		StateRoot: header.NewStateRoot,
	}

	// set metadata for this consensus state
	setConsensusMetadata(ctx, clientStore, header.GetHeight())

	return clientState, consensusState
}
