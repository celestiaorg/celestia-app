package types

import (
	"bytes"
	"errors"
	fmt "fmt"

	sdkclient "github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	"github.com/tendermint/tendermint/pkg/consts"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
)

var _ sdk.Msg = &MsgWirePayForMessage{}

// NewWirePayForMessage creates a new MsgWirePayForMessage by using the
// namespace and message to generate share commitments for the provided square sizes
// Note that the share commitments generated still need to be signed using the SignShareCommitments
// method.
func NewWirePayForMessage(namespace, message []byte, sizes ...uint64) (*MsgWirePayForMessage, error) {
	message = padMessage(message)
	out := &MsgWirePayForMessage{
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

// SignShareCommitments creates and signs MsgPayForMessages for each square size configured in the MsgWirePayForMessage
// to complete each shares commitment.
func (msg *MsgWirePayForMessage) SignShareCommitments(signer *KeyringSigner, options ...TxBuilderOption) error {
	msg.Signer = signer.GetSignerInfo().GetAddress().String()
	// create an entire MsgPayForMessage and signing over it, including the signature in each commitment
	for i, commit := range msg.MessageShareCommitment {
		builder := signer.NewTxBuilder()

		for _, option := range options {
			builder = option(builder)
		}

		sig, err := msg.createPayForMessageSignature(signer, builder, commit.K)
		if err != nil {
			return err
		}
		msg.MessageShareCommitment[i].Signature = sig
	}
	return nil
}

func (msg *MsgWirePayForMessage) Route() string { return RouterKey }

// ValidateBasic checks for valid namespace length, declared message size, share
// commitments, signatures for those share commitments, and fulfills the sdk.Msg
// interface
func (msg *MsgWirePayForMessage) ValidateBasic() error {

	// ensure that the namespace id is of length == NamespaceIDSize
	if nsLen := len(msg.GetMessageNameSpaceId()); nsLen != NamespaceIDSize {
		return fmt.Errorf(
			"invalid namespace length: got %d wanted %d",
			nsLen,
			NamespaceIDSize,
		)
	}

	if _, err := sdk.AccAddressFromBech32(msg.Signer); err != nil {
		return sdkerrors.ErrInvalidAddress.Wrapf("invalid 'from' address: %s", err)
	}

	// ensure that the included message is evenly divisble into shares
	if msgMod := uint64(len(msg.GetMessage())) % ShareSize; msgMod != 0 {
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

// GetSignBytes returns the bytes that are expected to be signed for the MsgWirePayForMessage.
// The signature of these bytes will never actually get included on chain. Note: instead the
// signature in the ShareCommitAndSignature of the appropriate square size is used
func (msg *MsgWirePayForMessage) GetSignBytes() []byte {
	return sdk.MustSortJSON(ModuleCdc.MustMarshalJSON(msg))
}

// GetSigners returns the addresses of the message signers
func (msg *MsgWirePayForMessage) GetSigners() []sdk.AccAddress {
	address, err := sdk.AccAddressFromBech32(msg.Signer)
	if err != nil {
		panic(err)
	}
	return []sdk.AccAddress{address}
}

// createPayForMessageSignature generates the signature for a PayForMessage for a single square
// size using the info from a MsgWirePayForMessage
func (msg *MsgWirePayForMessage) createPayForMessageSignature(signer *KeyringSigner, builder sdkclient.TxBuilder, k uint64) ([]byte, error) {
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

// unsignedPayForMessage use the data in the MsgWirePayForMessage
// to create a new MsgPayForMessage.
func (msg *MsgWirePayForMessage) unsignedPayForMessage(k uint64) (*MsgPayForMessage, error) {
	// create the commitment using the padded message
	commit, err := CreateCommitment(k, msg.MessageNameSpaceId, msg.Message)
	if err != nil {
		return nil, err
	}

	sPFM := MsgPayForMessage{
		MessageNamespaceId:     msg.MessageNameSpaceId,
		MessageSize:            msg.MessageSize,
		MessageShareCommitment: commit,
		Signer:                 msg.Signer,
	}
	return &sPFM, nil
}

// ProcessWirePayForMessage will perform the processing required by PreProcessTxs.
// It parses the MsgWirePayForMessage to produce the components needed to create a
// single  MsgPayForMessage
func ProcessWirePayForMessage(msg *MsgWirePayForMessage, squareSize uint64) (*tmproto.Message, *MsgPayForMessage, []byte, error) {
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
			fmt.Errorf("message does not commit to current square size: %d", squareSize)
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
