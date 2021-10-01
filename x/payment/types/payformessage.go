package types

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"

	"github.com/celestiaorg/nmt"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	"github.com/tendermint/tendermint/crypto/merkle"
	"github.com/tendermint/tendermint/pkg/consts"
	core "github.com/tendermint/tendermint/types"
)

const (
	TypeMsgPayforMessage    = "payformessage"
	TypeSignedPayForMessage = "SignedPayForMessage"
	ShareSize               = consts.ShareSize
	SquareSize              = consts.MaxSquareSize
	NamespaceIDSize         = consts.NamespaceSize
)

var _ sdk.Msg = &WirePayForMessage{}

// NewWirePayForMessage creates a new WirePayForMessage by using the
// namespace and message to generate share commitments for the provided square sizes
// Note that the share commitments generated still need to be signed using the Sign
// method
func NewWirePayForMessage(namespace, message []byte, gasPrice uint64, sizes ...uint64) (*WirePayForMessage, error) {
	message = PadMessage(message)
	out := &WirePayForMessage{
		MessageGasPrice:        gasPrice,
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
func (msg *WirePayForMessage) SignShareCommitments(accName string, ring keyring.Keyring, txCnfg client.TxConfig) error {
	// set signer
	accInfo, err := ring.Key(accName)
	if err != nil {
		return err
	}
	msg.Signer = accInfo.GetAddress().String()
	// sign each commitment todo(evan): refactor to use normal sdk.Txs
	for i, commit := range msg.MessageShareCommitment {
		bytesToSign, err := msg.GetCommitmentSignBytes(commit.K)
		if err != nil {
			return err
		}
		sig, _, err := ring.Sign(accName, bytesToSign)
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

// GetCommitmentSignBytes generates the bytes that each need to be signed per share commit
func (msg *WirePayForMessage) GetCommitmentSignBytes(k uint64) ([]byte, error) {
	sTxMsg, err := msg.SignedPayForMessage(k)
	if err != nil {
		return nil, err
	}
	return sTxMsg.GetSignBytes(), nil
}

// SignedPayForMessage use the data in the WirePayForMessage
// to create a new SignedPayForMessage
func (msg *WirePayForMessage) SignedPayForMessage(k uint64) (*SignedPayForMessage, error) {
	// create the commitment using the padded message
	commit, err := CreateCommitment(k, msg.MessageNameSpaceId, msg.Message)
	if err != nil {
		return nil, err
	}

	sTxMsg := SignedPayForMessage{
		MessageNamespaceId:     msg.MessageNameSpaceId,
		MessageSize:            msg.MessageSize,
		MessageShareCommitment: commit,
	}
	return &sTxMsg, nil
}

// ProcessWirePayForMessage will perform the processing required by PreProcessTxs for a set
// of sdk.Msg's from a single sdk.Tx
func ProcessWirePayForMessage(msg sdk.Msg, squareSize uint64) (core.Message, *SignedPayForMessage, error) {
	// reject all msgs in tx if a single included msg is not correct type
	wireMsg, ok := msg.(*WirePayForMessage)
	if !ok {
		return core.Message{},
			nil,
			errors.New("transaction contained a message type other than types.WirePayForMessage")
	}

	// make sure that a ShareCommitAndSignature of the correct size is
	// included in the message
	var shareCommit ShareCommitAndSignature
	for _, commit := range wireMsg.MessageShareCommitment {
		if commit.K == squareSize {
			shareCommit = commit
		}
	}
	// K == 0 means there was no share commit with the desired current square size
	if shareCommit.K == 0 {
		return core.Message{},
			nil,
			fmt.Errorf("No share commit for correct square size. Current square size: %d", squareSize)
	}

	// add the message to the list of core message to be returned to ll-core
	coreMsg := core.Message{
		NamespaceID: wireMsg.GetMessageNameSpaceId(),
		Data:        wireMsg.GetMessage(),
	}

	// wrap the signed transaction data
	signedPFM, err := wireMsg.SignedPayForMessage(squareSize)
	if err != nil {
		return core.Message{}, nil, err
	}

	return coreMsg, signedPFM, nil
}

var _ sdk.Msg = &SignedPayForMessage{}

// Route fullfills the sdk.Msg interface
func (msg *SignedPayForMessage) Route() string { return RouterKey }

// Type fullfills the sdk.Msg interface
func (msg *SignedPayForMessage) Type() string {
	return TypeSignedPayForMessage
}

// ValidateBasic fullfills the sdk.Msg interface by performing stateless
// validity checks on the msg that also don't require having the actual message
func (msg *SignedPayForMessage) ValidateBasic() error {
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
func (msg *SignedPayForMessage) GetSignBytes() []byte {
	return sdk.MustSortJSON(ModuleCdc.MustMarshalJSON(msg))
}

// GetSigners fullfills the sdk.Msg interface but does not return anything, as
// SignTransactionDataPayForMessage doesn't have access the public key necessary
// in WirePayForMessage
func (msg *SignedPayForMessage) GetSigners() []sdk.AccAddress {
	return []sdk.AccAddress{}
}

// BuildSignedPayForMessageTx creates an authsigning.Tx using the original tx and the signature
func BuildSignedPayForMessageTx(
	origTx authsigning.Tx,
	msg *SignedPayForMessage,
	builder client.TxBuilder,
	signature []byte,
) (authsigning.Tx, error) {
	err := builder.SetMsgs(msg)
	if err != nil {
		return nil, err
	}
	builder.SetGasLimit(origTx.GetGas())
	builder.SetFeeAmount(origTx.GetFee())
	sigs, err := origTx.GetSignaturesV2()
	if err != nil {
		return nil, err
	}
	if len(sigs) != 1 {
		return nil, fmt.Errorf("unexpected number of signatures: expected 1 got %d", len(sigs))
	}
	newSigningData := signing.SingleSignatureData{
		SignMode:  signing.SignMode_SIGN_MODE_DIRECT,
		Signature: signature,
	}
	sigs[0].Data = newSigningData
	builder.SetSignatures(sigs[0])
	return builder.GetTx(), nil
}

// CreateCommitment generates the commit bytes for a given message, namespace, and
// squaresize using a namespace merkle tree and the rules described at
// https://github.com/celestiaorg/celestia-specs/blob/master/rationale/message_block_layout.md#non-interactive-default-rules
func CreateCommitment(k uint64, namespace, message []byte) ([]byte, error) {
	// add padding to the message if necessary
	message = PadMessage(message)

	// break message into shares
	shares := chunkMessage(message)

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
