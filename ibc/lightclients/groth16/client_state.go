package groth16

import (
	"bytes"
	fmt "fmt"

	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"

	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/backend/groth16"
	clienttypes "github.com/cosmos/ibc-go/v6/modules/core/02-client/types"
	connectiontypes "github.com/cosmos/ibc-go/v6/modules/core/03-connection/types"
	channeltypes "github.com/cosmos/ibc-go/v6/modules/core/04-channel/types"
	commitmenttypes "github.com/cosmos/ibc-go/v6/modules/core/23-commitment/types"
	host "github.com/cosmos/ibc-go/v6/modules/core/24-host"
	"github.com/cosmos/ibc-go/v6/modules/core/exported"
)

const (
	Groth16ClientType = "groth16"
)

var _ exported.ClientState = (*ClientState)(nil)

// NewClientState creates a new ClientState instance
func NewClientState(
	latestHeight uint64,
	stateTransitionVerifierKey, stateInclusionVerifierKey []byte,
) *ClientState {
	return &ClientState{
		LatestHeight:               latestHeight,
		StateTransitionVerifierKey: stateTransitionVerifierKey,
		StateInclusionVerifierKey:  stateInclusionVerifierKey,
	}
}

// ClientType is groth16.
func (cs ClientState) ClientType() string {
	return Groth16ClientType
}

// GetLatestHeight returns latest block height.
func (cs ClientState) GetLatestHeight() exported.Height {
	return clienttypes.Height{
		RevisionNumber: 0,
		RevisionHeight: cs.LatestHeight,
	}
}

// Status returns the status of the groth16 client.
func (cs ClientState) Status(
	ctx sdk.Context,
	clientStore sdk.KVStore,
	cdc codec.BinaryCodec,
) exported.Status {
	return exported.Active
}

// Validate performs a basic validation of the client state fields.
func (cs ClientState) Validate() error {
	if cs.StateTransitionVerifierKey == nil {
		return sdkerrors.Wrap(clienttypes.ErrInvalidClient, "state transition or inclusion verifier key is nil")
	}
	if cs.StateInclusionVerifierKey == nil {
		return sdkerrors.Wrap(clienttypes.ErrInvalidClient, "state inclusion verifier key is nil")
	}
	return nil
}

// ZeroCustomFields returns a ClientState that is a copy of the current ClientState
// with all client customizable fields zeroed out
func (cs ClientState) ZeroCustomFields() exported.ClientState {
	// Copy over all chain-specified fields
	// and leave custom fields empty
	return &ClientState{
		LatestHeight:               cs.LatestHeight,
		StateTransitionVerifierKey: cs.StateTransitionVerifierKey,
		StateInclusionVerifierKey:  cs.StateInclusionVerifierKey,
	}
}

// Initialize will check that initial consensus state is a Tendermint consensus state
// and will store ProcessedTime for initial consensus state as ctx.BlockTime()
func (cs ClientState) Initialize(ctx sdk.Context, _ codec.BinaryCodec, clientStore sdk.KVStore, consState exported.ConsensusState) error {
	if _, ok := consState.(*ConsensusState); !ok {
		return sdkerrors.Wrapf(clienttypes.ErrInvalidConsensus, "invalid initial consensus state. expected type: %T, got: %T",
			&ConsensusState{}, consState)
	}
	// set metadata for initial consensus state.
	setConsensusMetadata(ctx, clientStore, cs.GetLatestHeight())
	return nil
}

// VerifyClientState verifies a proof of the client state of the running chain
// stored on the target machine
func (cs ClientState) VerifyClientState(
	store sdk.KVStore,
	cdc codec.BinaryCodec,
	height exported.Height,
	prefix exported.Prefix,
	counterpartyClientIdentifier string,
	proof []byte,
	clientState exported.ClientState,
) error {
	clientPrefixedPath := commitmenttypes.NewMerklePath(host.FullClientStatePath(counterpartyClientIdentifier))
	path, err := commitmenttypes.ApplyPrefix(prefix, clientPrefixedPath)
	if err != nil {
		return err
	}

	if clientState == nil {
		return sdkerrors.Wrap(clienttypes.ErrInvalidClient, "client state cannot be empty")
	}

	_, ok := clientState.(*ClientState)
	if !ok {
		return sdkerrors.Wrapf(clienttypes.ErrInvalidClient, "invalid client type %T, expected %T", clientState, &ClientState{})
	}

	bz, err := cdc.MarshalInterface(clientState)
	if err != nil {
		return err
	}

	return cs.VerifyMembership(store, cdc, height, 0, 0, proof, path, bz)
}

// VerifyClientConsensusState verifies a proof of the consensus state of the
// Tendermint client stored on the target machine.
func (cs ClientState) VerifyClientConsensusState(
	store sdk.KVStore,
	cdc codec.BinaryCodec,
	height exported.Height,
	counterpartyClientIdentifier string,
	consensusHeight exported.Height,
	prefix exported.Prefix,
	proof []byte,
	consensusState exported.ConsensusState,
) error {
	clientPrefixedPath := commitmenttypes.NewMerklePath(host.FullConsensusStatePath(counterpartyClientIdentifier, consensusHeight))
	path, err := commitmenttypes.ApplyPrefix(prefix, clientPrefixedPath)
	if err != nil {
		return err
	}

	if consensusState == nil {
		return sdkerrors.Wrap(clienttypes.ErrInvalidConsensus, "consensus state cannot be empty")
	}

	_, ok := consensusState.(*ConsensusState)
	if !ok {
		return sdkerrors.Wrapf(clienttypes.ErrInvalidConsensus, "invalid consensus type %T, expected %T", consensusState, &ConsensusState{})
	}

	bz, err := cdc.MarshalInterface(consensusState)
	if err != nil {
		return err
	}

	return cs.VerifyMembership(store, cdc, height, 0, 0, proof, path, bz)
}

// VerifyConnectionState verifies a proof of the connection state of the
// specified connection end stored on the target machine.
func (cs ClientState) VerifyConnectionState(
	store sdk.KVStore,
	cdc codec.BinaryCodec,
	height exported.Height,
	prefix exported.Prefix,
	proof []byte,
	connectionID string,
	connectionEnd exported.ConnectionI,
) error {

	connectionPath := commitmenttypes.NewMerklePath(host.ConnectionPath(connectionID))
	path, err := commitmenttypes.ApplyPrefix(prefix, connectionPath)
	if err != nil {
		return err
	}

	connection, ok := connectionEnd.(connectiontypes.ConnectionEnd)
	if !ok {
		return sdkerrors.Wrapf(sdkerrors.ErrInvalidType, "invalid connection type %T", connectionEnd)
	}

	bz, err := cdc.Marshal(&connection)
	if err != nil {
		return err
	}

	return cs.VerifyMembership(store, cdc, height, 0, 0, proof, path, bz)
}

// VerifyChannelState verifies a proof of the channel state of the specified
// channel end, under the specified port, stored on the target machine.
func (cs ClientState) VerifyChannelState(
	store sdk.KVStore,
	cdc codec.BinaryCodec,
	height exported.Height,
	prefix exported.Prefix,
	proof []byte,
	portID,
	channelID string,
	channel exported.ChannelI,
) error {
	channelPath := commitmenttypes.NewMerklePath(host.ChannelPath(portID, channelID))
	path, err := commitmenttypes.ApplyPrefix(prefix, channelPath)
	if err != nil {
		return err
	}

	channelEnd, ok := channel.(channeltypes.Channel)
	if !ok {
		return sdkerrors.Wrapf(sdkerrors.ErrInvalidType, "invalid channel type %T", channel)
	}

	bz, err := cdc.Marshal(&channelEnd)
	if err != nil {
		return err
	}

	return cs.VerifyMembership(store, cdc, height, 0, 0, proof, path, bz)
}

// VerifyPacketCommitment verifies a proof of an outgoing packet commitment at
// the specified port, specified channel, and specified sequence.
func (cs ClientState) VerifyPacketCommitment(
	ctx sdk.Context,
	store sdk.KVStore,
	cdc codec.BinaryCodec,
	height exported.Height,
	delayTimePeriod uint64,
	delayBlockPeriod uint64,
	prefix exported.Prefix,
	proof []byte,
	portID,
	channelID string,
	sequence uint64,
	commitmentBytes []byte,
) error {
	// check delay period has passed
	if err := verifyDelayPeriodPassed(ctx, store, height, delayTimePeriod, delayBlockPeriod); err != nil {
		return err
	}

	commitmentPath := commitmenttypes.NewMerklePath(host.PacketCommitmentPath(portID, channelID, sequence))
	path, err := commitmenttypes.ApplyPrefix(prefix, commitmentPath)
	if err != nil {
		return err
	}

	return cs.VerifyMembership(store, cdc, height, delayTimePeriod, delayBlockPeriod, proof, path, commitmentBytes)
}

// VerifyPacketAcknowledgement verifies a proof of an incoming packet
// acknowledgement at the specified port, specified channel, and specified sequence.
func (cs ClientState) VerifyPacketAcknowledgement(
	ctx sdk.Context,
	store sdk.KVStore,
	cdc codec.BinaryCodec,
	height exported.Height,
	delayTimePeriod uint64,
	delayBlockPeriod uint64,
	prefix exported.Prefix,
	proof []byte,
	portID,
	channelID string,
	sequence uint64,
	acknowledgement []byte,
) error {
	// check delay period has passed
	if err := verifyDelayPeriodPassed(ctx, store, height, delayTimePeriod, delayBlockPeriod); err != nil {
		return err
	}

	ackPath := commitmenttypes.NewMerklePath(host.PacketAcknowledgementPath(portID, channelID, sequence))
	path, err := commitmenttypes.ApplyPrefix(prefix, ackPath)
	if err != nil {
		return err
	}

	return cs.VerifyMembership(store, cdc, height, delayTimePeriod, delayBlockPeriod, proof, path, channeltypes.CommitAcknowledgement(acknowledgement))
}

// VerifyPacketReceiptAbsence verifies a proof of the absence of an
// incoming packet receipt at the specified port, specified channel, and
// specified sequence.
func (cs ClientState) VerifyPacketReceiptAbsence(
	ctx sdk.Context,
	store sdk.KVStore,
	cdc codec.BinaryCodec,
	height exported.Height,
	delayTimePeriod uint64,
	delayBlockPeriod uint64,
	prefix exported.Prefix,
	proof []byte,
	portID,
	channelID string,
	sequence uint64,
) error {
	// check delay period has passed
	if err := verifyDelayPeriodPassed(ctx, store, height, delayTimePeriod, delayBlockPeriod); err != nil {
		return err
	}

	receiptPath := commitmenttypes.NewMerklePath(host.PacketReceiptPath(portID, channelID, sequence))
	path, err := commitmenttypes.ApplyPrefix(prefix, receiptPath)
	if err != nil {
		return err
	}

	return cs.VerifyNonMembership(store, cdc, height, delayTimePeriod, delayBlockPeriod, proof, path)
}

// VerifyNextSequenceRecv verifies a proof of the next sequence number to be
// received of the specified channel at the specified port.
func (cs ClientState) VerifyNextSequenceRecv(
	ctx sdk.Context,
	store sdk.KVStore,
	cdc codec.BinaryCodec,
	height exported.Height,
	delayTimePeriod uint64,
	delayBlockPeriod uint64,
	prefix exported.Prefix,
	proof []byte,
	portID,
	channelID string,
	nextSequenceRecv uint64,
) error {
	// check delay period has passed
	if err := verifyDelayPeriodPassed(ctx, store, height, delayTimePeriod, delayBlockPeriod); err != nil {
		return err
	}

	nextSequenceRecvPath := commitmenttypes.NewMerklePath(host.NextSequenceRecvPath(portID, channelID))
	path, err := commitmenttypes.ApplyPrefix(prefix, nextSequenceRecvPath)
	if err != nil {
		return err
	}

	bz := sdk.Uint64ToBigEndian(nextSequenceRecv)
	return cs.VerifyMembership(store, cdc, height, delayTimePeriod, delayBlockPeriod, proof, path, bz)
}

// verifyDelayPeriodPassed will ensure that at least delayTimePeriod amount of time and delayBlockPeriod number of blocks have passed
// since consensus state was submitted before allowing verification to continue.
func verifyDelayPeriodPassed(ctx sdk.Context, store sdk.KVStore, proofHeight exported.Height, delayTimePeriod, delayBlockPeriod uint64) error {
	// check that executing chain's timestamp has passed consensusState's processed time + delay time period
	processedTime, ok := GetProcessedTime(store, proofHeight)
	if !ok {
		return sdkerrors.Wrapf(ErrProcessedTimeNotFound, "processed time not found for height: %s", proofHeight)
	}
	currentTimestamp := uint64(ctx.BlockTime().UnixNano())
	validTime := processedTime + delayTimePeriod
	// NOTE: delay time period is inclusive, so if currentTimestamp is validTime, then we return no error
	if currentTimestamp < validTime {
		return sdkerrors.Wrapf(ErrDelayPeriodNotPassed, "cannot verify packet until time: %d, current time: %d",
			validTime, currentTimestamp)
	}
	// check that executing chain's height has passed consensusState's processed height + delay block period
	processedHeight, ok := GetProcessedHeight(store, proofHeight)
	if !ok {
		return sdkerrors.Wrapf(ErrProcessedHeightNotFound, "processed height not found for height: %s", proofHeight)
	}
	currentHeight := clienttypes.GetSelfHeight(ctx)
	validHeight := clienttypes.NewHeight(processedHeight.GetRevisionNumber(), processedHeight.GetRevisionHeight()+delayBlockPeriod)
	// NOTE: delay block period is inclusive, so if currentHeight is validHeight, then we return no error
	if currentHeight.LT(validHeight) {
		return sdkerrors.Wrapf(ErrDelayPeriodNotPassed, "cannot verify packet until height: %s, current height: %s",
			validHeight, currentHeight)
	}
	return nil
}

//------------------------------------

// The following are modified methods from the v9 IBC Client interface. The idea is to make
// it easy to update this client once Celestia moves to v9 of IBC

func (cs ClientState) VerifyMembership(
	clientStore sdk.KVStore,
	cdc codec.BinaryCodec,
	height exported.Height,
	delayTimePeriod uint64,
	delayBlockPeriod uint64,
	proof []byte,
	path exported.Path,
	value []byte,
) error {
	groth16Proof := groth16.NewProof(ecc.BN254)

	_, err := groth16Proof.ReadFrom(bytes.NewReader(proof))
	if err != nil {
		return fmt.Errorf("failed to deserialize proof: %w", err)
	}

	vk, err := cs.GetStateInclusionVerifierKey()
	if err != nil {
		return fmt.Errorf("failed to get state transition verifier key: %w", err)
	}

	consensusState, err := GetConsensusState(clientStore, cdc, height)
	if err != nil {
		return fmt.Errorf("failed to get consensus state: %w", err)
	}

	merklePath, ok := path.(commitmenttypes.MerklePath)
	if !ok {
		return sdkerrors.Wrapf(sdkerrors.ErrInvalidType, "expected %T, got %T", commitmenttypes.MerklePath{}, path)
	}

	publicWitness, err := GenerateStateInclusionPublicWitness(consensusState.StateRoot, merklePath.KeyPath, value)
	if err != nil {
		return fmt.Errorf("failed to generate state inclusion public witness: %w", err)
	}

	if err := groth16.Verify(groth16Proof, vk, publicWitness); err != nil {
		return fmt.Errorf("failed to verify state inclusion proof: %w", err)
	}
	return nil
}

// VerifyNonMembership verifies a proof of the absence of a key in the Merkle tree.
// It's the same as VerifyMembership, but the value is nil
func (cs ClientState) VerifyNonMembership(
	clientStore sdk.KVStore,
	cdc codec.BinaryCodec,
	height exported.Height,
	delayTimePeriod uint64,
	delayBlockPeriod uint64,
	proof []byte,
	path exported.Path,
) error {
	groth16Proof := groth16.NewProof(ecc.BN254)

	_, err := groth16Proof.ReadFrom(bytes.NewReader(proof))
	if err != nil {
		return fmt.Errorf("failed to deserialize proof: %w", err)
	}

	vk, err := cs.GetStateInclusionVerifierKey()
	if err != nil {
		return fmt.Errorf("failed to get state transition verifier key: %w", err)
	}

	consensusState, err := GetConsensusState(clientStore, cdc, height)
	if err != nil {
		return fmt.Errorf("failed to get consensus state: %w", err)
	}

	merklePath, ok := path.(commitmenttypes.MerklePath)
	if !ok {
		return sdkerrors.Wrapf(sdkerrors.ErrInvalidType, "expected %T, got %T", commitmenttypes.MerklePath{}, path)
	}

	publicWitness, err := GenerateStateInclusionPublicWitness(consensusState.StateRoot, merklePath.KeyPath, nil)
	if err != nil {
		return fmt.Errorf("failed to generate state inclusion public witness: %w", err)
	}

	if err := groth16.Verify(groth16Proof, vk, publicWitness); err != nil {
		return fmt.Errorf("failed to verify state inclusion proof: %w", err)
	}
	return nil
}

//------------------------------------

// Checking for misbehaviour is a noop for groth16
func (cs ClientState) CheckMisbehaviourAndUpdateState(
	ctx sdk.Context,
	cdc codec.BinaryCodec,
	clientStore sdk.KVStore,
	misbehaviour exported.Misbehaviour,
) (exported.ClientState, error) {
	return &cs, nil
}

func (cs ClientState) CheckSubstituteAndUpdateState(
	ctx sdk.Context, cdc codec.BinaryCodec, subjectClientStore,
	substituteClientStore sdk.KVStore, substituteClient exported.ClientState,
) (exported.ClientState, error) {
	return nil, sdkerrors.Wrap(clienttypes.ErrUpdateClientFailed, "cannot update groth16 client with a proposal")
}
