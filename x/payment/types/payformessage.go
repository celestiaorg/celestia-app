package types

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"

	"github.com/celestiaorg/nmt"
	sdkclient "github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	"github.com/tendermint/tendermint/crypto/merkle"
	"github.com/tendermint/tendermint/pkg/consts"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
)

const (
	TypeMsgPayforMessage = "payformessage"
	TypePayForMessage    = "PayForMessage"
	ShareSize            = consts.ShareSize
	SquareSize           = consts.MaxSquareSize
	NamespaceIDSize      = consts.NamespaceSize
)

var _ sdk.Msg = &WirePayForMessage{}

// NewWirePayForMessage creates a new WirePayForMessage by using the
// namespace and message to generate share commitments for the provided square sizes
// Note that the share commitments generated still need to be signed using the Sign
// method
func NewWirePayForMessage(namespace, message []byte, sizes ...uint64) (*WirePayForMessage, error) {
	message = PadMessage(message)
	out := &WirePayForMessage{
		MessageNameSpaceId:     namespace,
		MessageSize:            uint64(len(message)),
		Message:                message,
		MessageShareCommitment: make([]ShareCommitAndSignature, len(sizes)),
	}

	// generate the share commitments
	for i, size := range sizes {
		commit, err := CreateCommitment(size, namespace, message)
		if err != nil {
			return nil, err
		}
		out.MessageShareCommitment[i] = ShareCommitAndSignature{K: size, ShareCommitment: commit}
	}
	return out, nil
}

// SignShareCommitments use the provided Keyring to sign each of the share commits
// generated during the creation of the WirePayForMessage
func (msg *WirePayForMessage) SignShareCommitments(signer *KeyringSigner, builder sdkclient.TxBuilder) error {
	msg.Signer = signer.GetSignerInfo().GetAddress().String()
	// sign each commitment by creating an entire
	for i, commit := range msg.MessageShareCommitment {
		sig, err := msg.createPayForMessageSignature(signer, builder, commit.K)
		if err != nil {
			return err
		}
		msg.MessageShareCommitment[i].Signature = sig
	}
	return nil
}

func (msg *WirePayForMessage) Route() string { return RouterKey }

// ValidateBasic checks for valid namespace length, declared message size, share
// commitments, signatures for those share commitments, and fulfills the sdk.Msg
// interface
func (msg *WirePayForMessage) ValidateBasic() error {

	// ensure that the namespace id is of length == NamespaceIDSize
	if len(msg.GetMessageNameSpaceId()) != NamespaceIDSize {
		return fmt.Errorf(
			"invalid namespace length: got %d wanted %d",
			len(msg.GetMessageNameSpaceId()),
			NamespaceIDSize,
		)
	}

	if _, err := sdk.AccAddressFromBech32(msg.Signer); err != nil {
		return sdkerrors.ErrInvalidAddress.Wrapf("invalid 'from' address: %s", err)
	}

	// ensure that the included message is evenly divisble into shares
	if uint64(len(msg.GetMessage()))%ShareSize != 0 {
		return fmt.Errorf("Share message must be divisible by %d", ShareSize)
	}

	// make sure that the message size matches the actual size of the message
	if msg.MessageSize != uint64(len(msg.Message)) {
		return fmt.Errorf(
			"Declared Message size does not match actual Message size, %d vs %d",
			msg.MessageSize,
			len(msg.Message),
		)
	}

	// ensure that a reserved namespace is not used
	if bytes.Compare(msg.GetMessageNameSpaceId(), consts.MaxReservedNamespace) < 1 {
		return errors.New("message is not valid: uses a reserved namesapce ID")
	}

	for _, commit := range msg.MessageShareCommitment {
		// check that each commit is valid
		calculatedCommit, err := CreateCommitment(commit.K, msg.GetMessageNameSpaceId(), msg.Message)
		if err != nil {
			return err
		}

		if string(calculatedCommit) != string(commit.ShareCommitment) {
			return fmt.Errorf("invalid commit for square size %d", commit.K)
		}
	}

	return nil
}

// GetSignBytes returns the bytes that are expected to be signed for the WirePayForMessage.
// The signature of these bytes will never actually get included on chain. Note: instead the
// signature in the ShareCommitAndSignature of the appropriate square size is used
func (msg *WirePayForMessage) GetSignBytes() []byte {
	return sdk.MustSortJSON(ModuleCdc.MustMarshalJSON(msg))
}

// GetSigners returns the addresses of the message signers
func (msg *WirePayForMessage) GetSigners() []sdk.AccAddress {
	address, err := sdk.AccAddressFromBech32(msg.Signer)
	if err != nil {
		panic(err)
	}
	return []sdk.AccAddress{address}
}

// createPayForMessageSignature generates the bytes that each need to be signed per share commit
func (msg *WirePayForMessage) createPayForMessageSignature(signer *KeyringSigner, builder sdkclient.TxBuilder, k uint64) ([]byte, error) {
	pfm, err := msg.unsignedPayForMessage(k)
	if err != nil {
		return nil, err
	}
	tx, err := signer.BuildSignedTx(builder, pfm)
	if err != nil {
		return nil, err
	}
	sigs, err := tx.GetSignaturesV2()
	if err != nil {
		return nil, err
	}
	if len(sigs) != 1 {
		return nil, fmt.Errorf("expected a single signer: got %d", len(sigs))
	}
	sig, ok := sigs[0].Data.(*signing.SingleSignatureData)
	if !ok {
		return nil, fmt.Errorf("expected a single signer")
	}
	return sig.Signature, nil
}

// unsignedPayForMessage use the data in the WirePayForMessage
// to create a new PayForMessage
func (msg *WirePayForMessage) unsignedPayForMessage(k uint64) (*PayForMessage, error) {
	// create the commitment using the padded message
	commit, err := CreateCommitment(k, msg.MessageNameSpaceId, msg.Message)
	if err != nil {
		return nil, err
	}

	sPFM := PayForMessage{
		MessageNamespaceId:     msg.MessageNameSpaceId,
		MessageSize:            msg.MessageSize,
		MessageShareCommitment: commit,
		Signer:                 msg.Signer,
	}
	return &sPFM, nil
}

// ProcessWirePayForMessage will perform the processing required by PreProcessTxs.
// It parses the WirePayForMessage to produce the components needed to create a
// single PayForMessage
func ProcessWirePayForMessage(msg *WirePayForMessage, squareSize uint64) (*tmproto.Message, *PayForMessage, []byte, error) {
	// make sure that a ShareCommitAndSignature of the correct size is
	// included in the message
	var shareCommit *ShareCommitAndSignature
	for _, commit := range msg.MessageShareCommitment {
		if commit.K == squareSize {
			shareCommit = &commit
		}
	}
	if shareCommit == nil {
		return nil,
			nil,
			nil,
			fmt.Errorf("No share commit for correct square size. Current square size: %d", squareSize)
	}

	// add the message to the list of core message to be returned to ll-core
	coreMsg := tmproto.Message{
		NamespaceId: msg.GetMessageNameSpaceId(),
		Data:        msg.GetMessage(),
	}

	// wrap the signed transaction data
	pfm, err := msg.unsignedPayForMessage(squareSize)
	if err != nil {
		return nil, nil, nil, err
	}

	return &coreMsg, pfm, shareCommit.Signature, nil
}

var _ sdk.Msg = &PayForMessage{}

// Route fullfills the sdk.Msg interface
func (msg *PayForMessage) Route() string { return RouterKey }

// Type fullfills the sdk.Msg interface
func (msg *PayForMessage) Type() string {
	return TypePayForMessage
}

// ValidateBasic fullfills the sdk.Msg interface by performing stateless
// validity checks on the msg that also don't require having the actual message
func (msg *PayForMessage) ValidateBasic() error {
	// ensure that the namespace id is of length == NamespaceIDSize
	if len(msg.GetMessageNamespaceId()) != NamespaceIDSize {
		return fmt.Errorf(
			"invalid namespace length: got %d wanted %d",
			len(msg.GetMessageNamespaceId()),
			NamespaceIDSize,
		)
	}
	return nil
}

// GetSignBytes fullfills the sdk.Msg interface by reterning a deterministic set
// of bytes to sign over
func (msg *PayForMessage) GetSignBytes() []byte {
	return sdk.MustSortJSON(ModuleCdc.MustMarshalJSON(msg))
}

// GetSigners fullfills the sdk.Msg interface by returning the signer's address
func (msg *PayForMessage) GetSigners() []sdk.AccAddress {
	address, err := sdk.AccAddressFromBech32(msg.Signer)
	if err != nil {
		panic(err)
	}
	return []sdk.AccAddress{address}
}

// BuildPayForMessageTxFromWireTx creates an authsigning.Tx using data from the original
// WirePayForMessage sdk.Tx and the signature provided. This is used while processing
// the WirePayForMessages into Signed PayForMessage
func BuildPayForMessageTxFromWireTx(
	origTx authsigning.Tx,
	builder sdkclient.TxBuilder,
	signature []byte,
	msg *PayForMessage,
) (authsigning.Tx, error) {
	err := builder.SetMsgs(msg)
	if err != nil {
		return nil, err
	}
	builder.SetGasLimit(origTx.GetGas())
	builder.SetFeeAmount(origTx.GetFee())

	origSigs, err := origTx.GetSignaturesV2()
	if err != nil {
		return nil, err
	}
	if len(origSigs) != 1 {
		return nil, fmt.Errorf("unexpected number of signers: %d", len(origSigs))
	}

	newSig := signing.SignatureV2{
		PubKey: origSigs[0].PubKey,
		Data: &signing.SingleSignatureData{
			SignMode:  signing.SignMode_SIGN_MODE_DIRECT,
			Signature: signature,
		},
		Sequence: origSigs[0].Sequence,
	}

	err = builder.SetSignatures(newSig)
	if err != nil {
		return nil, err
	}

	return builder.GetTx(), nil
}

// CreateCommitment generates the commit bytes for a given message, namespace, and
// squaresize using a namespace merkle tree and the rules described at
// https://github.com/celestiaorg/celestia-specs/blob/master/src/rationale/message_block_layout.md#message-layout-rationale
func CreateCommitment(k uint64, namespace, message []byte) ([]byte, error) {
	// add padding to the message if necessary
	message = PadMessage(message)

	// break message into shares
	shares := chunkMessage(message)
	// if the number of shares is larger than that in the square, throw an error
	// note, we use k*k-1 here because at least a single share will be reserved
	// for the transaction paying for the message, therefore the max number of
	// shares a message can be is number of shares in square -1.
	if uint64(len(shares)) > k*k-1 {
		return nil, fmt.Errorf("message size exceeds square size")
	}

	// organize shares for merkle mountain range
	heights := PowerOf2MountainRange(uint64(len(shares)), k)
	leafSets := make([][][]byte, len(heights))
	cursor := uint64(0)
	for i, height := range heights {
		leafSets[i] = shares[cursor : cursor+height]
		cursor = cursor + height
	}

	// create the commits by pushing each leaf set onto an nmt
	subTreeRoots := make([][]byte, len(leafSets))
	for i, set := range leafSets {
		// create the nmt todo(evan) use nmt wrapper
		tree := nmt.New(sha256.New(), nmt.NamespaceIDSize(NamespaceIDSize))
		for _, leaf := range set {
			nsLeaf := append(make([]byte, 0), append(namespace, leaf...)...)
			err := tree.Push(nsLeaf)
			if err != nil {
				return nil, err
			}
		}
		// add the root
		subTreeRoots[i] = tree.Root()
	}
	return merkle.HashFromByteSlices(subTreeRoots), nil
}

// chunkMessage breaks the message into ShareSize pieces
func chunkMessage(message []byte) [][]byte {
	var shares [][]byte
	for i := 0; i < len(message); i += ShareSize {
		end := i + ShareSize
		if end > len(message) {
			end = len(message)
		}
		shares = append(shares, message[i:end])
	}
	return shares
}

// PadMessage adds padding to the msg if the length of the msg is not divisible
// by the share size specified in celestia-core
func PadMessage(msg []byte) []byte {
	// check if the message needs padding
	if uint64(len(msg))%ShareSize == 0 {
		return msg
	}

	shareCount := (len(msg) / ShareSize) + 1

	padded := make([]byte, shareCount*ShareSize)
	copy(padded, msg)
	return padded
}

// PowerOf2MountainRange returns the heights of the subtrees for binary merkle
// mountian range
func PowerOf2MountainRange(l, k uint64) []uint64 {
	var output []uint64

	for l != 0 {
		switch {
		case l >= k:
			output = append(output, k)
			l = l - k
		case l < k:
			p := nextPowerOf2(l)
			output = append(output, p)
			l = l - p
		}
	}

	return output
}

// nextPowerOf2 returns the next lowest power of 2 unless the input is a power
// of two, in which case it returns the input
func nextPowerOf2(v uint64) uint64 {
	if v == 1 {
		return 1
	}
	// keep track of the input
	i := v

	// find the next highest power using bit mashing
	v--
	v |= v >> 1
	v |= v >> 2
	v |= v >> 4
	v |= v >> 8
	v |= v >> 16
	v |= v >> 32
	v++

	// check if the input was the next highest power
	if i == v {
		return v
	}

	// return the next lowest power
	return v / 2
}
