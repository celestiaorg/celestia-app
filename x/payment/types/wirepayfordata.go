package types

import (
	"bytes"
	fmt "fmt"

	sdkclient "github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	"github.com/tendermint/tendermint/pkg/consts"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
)

var _ sdk.Msg = &MsgWirePayForData{}

// NewWirePayForData creates a new MsgWirePayForData by using the
// namespace and message to generate share commitments for the provided square sizes
// Note that the share commitments generated still need to be signed using the SignShareCommitments
// method.
func NewWirePayForData(namespace, message []byte, sizes ...uint64) (*MsgWirePayForData, error) {
	message = padMessage(message)
	out := &MsgWirePayForData{
		MessageNameSpaceId:     namespace,
		MessageSize:            uint64(len(message)),
		Message:                message,
		MessageShareCommitment: make([]ShareCommitAndSignature, len(sizes)),
	}

	// generate the share commitments
	for i, size := range sizes {
		if !powerOf2(size) {
			return nil, fmt.Errorf("invalid square size, the size must be power of 2: %d", size)
		}
		commit, err := CreateCommitment(size, namespace, message)
		if err != nil {
			return nil, err
		}
		out.MessageShareCommitment[i] = ShareCommitAndSignature{K: size, ShareCommitment: commit}
	}
	return out, nil
}

// SignShareCommitments creates and signs MsgPayForDatas for each square size configured in the MsgWirePayForData
// to complete each shares commitment.
func (msg *MsgWirePayForData) SignShareCommitments(signer *KeyringSigner, options ...TxBuilderOption) error {
	msg.Signer = signer.GetSignerInfo().GetAddress().String()
	// create an entire MsgPayForData and signing over it, including the signature in each commitment
	for i, commit := range msg.MessageShareCommitment {
		builder := signer.NewTxBuilder()

		for _, option := range options {
			builder = option(builder)
		}

		sig, err := msg.createPayForDataSignature(signer, builder, commit.K)
		if err != nil {
			return err
		}
		msg.MessageShareCommitment[i].Signature = sig
	}
	return nil
}

func (msg *MsgWirePayForData) Route() string { return RouterKey }

// ValidateBasic checks for valid namespace length, declared message size, share
// commitments, signatures for those share commitments, and fulfills the sdk.Msg
// interface.
func (msg *MsgWirePayForData) ValidateBasic() error {

	// ensure that the namespace id is of length == NamespaceIDSize
	if nsLen := len(msg.GetMessageNameSpaceId()); nsLen != NamespaceIDSize {
		return ErrInvalidNamespaceLen.Wrapf("got: %d want: %d",
			nsLen,
			NamespaceIDSize,
		)
	}

	if _, err := sdk.AccAddressFromBech32(msg.Signer); err != nil {
		return sdkerrors.ErrInvalidAddress.Wrapf("invalid 'from' address: %s", err)
	}

	// ensure that the included message is evenly divisible into shares
	if msgMod := uint64(len(msg.GetMessage())) % ShareSize; msgMod != 0 {
		return ErrInvalidDataSize.Wrapf(
			"shareSize: %d, data length: %d",
			len(msg.Message),
			ShareSize,
		)
	}

	// make sure that the message size matches the actual size of the message
	if msg.MessageSize != uint64(len(msg.Message)) {
		return ErrDeclaredActualDataSizeMismatch.Wrapf(
			"declared: %d vs actual: %d",
			msg.MessageSize,
			len(msg.Message),
		)
	}

	// ensure that a reserved namespace is not used
	if bytes.Compare(msg.GetMessageNameSpaceId(), consts.MaxReservedNamespace) < 1 {
		return ErrReservedNamespace.Wrapf("got namespace: %x, want: > %x", msg.GetMessageNameSpaceId(), consts.MaxReservedNamespace)
	}

	for idx, commit := range msg.MessageShareCommitment {
		// check that each commit is valid
		if !powerOf2(commit.K) {
			return ErrCommittedSquareSizeNotPowOf2.Wrapf("committed to square size: %d", commit.K)
		}

		calculatedCommit, err := CreateCommitment(commit.K, msg.GetMessageNameSpaceId(), msg.Message)
		if err != nil {
			return ErrCalculateCommit.Wrap(err.Error())
		}

		if !bytes.Equal(calculatedCommit, commit.ShareCommitment) {
			return ErrInvalidShareCommit.Wrapf("for square size %d and commit number %v", commit.K, idx)
		}
	}

	return nil
}

// GetSignBytes returns the bytes that are expected to be signed for the MsgWirePayForData.
// The signature of these bytes will never actually get included on chain. Note: instead the
// signature in the ShareCommitAndSignature of the appropriate square size is used.
func (msg *MsgWirePayForData) GetSignBytes() []byte {
	return sdk.MustSortJSON(ModuleCdc.MustMarshalJSON(msg))
}

// GetSigners returns the addresses of the message signers
func (msg *MsgWirePayForData) GetSigners() []sdk.AccAddress {
	address, err := sdk.AccAddressFromBech32(msg.Signer)
	if err != nil {
		panic(err)
	}
	return []sdk.AccAddress{address}
}

// createPayForDataSignature generates the signature for a PayForData for a single square
// size using the info from a MsgWirePayForData.
func (msg *MsgWirePayForData) createPayForDataSignature(signer *KeyringSigner, builder sdkclient.TxBuilder, k uint64) ([]byte, error) {
	pfd, err := msg.unsignedPayForData(k)
	if err != nil {
		return nil, err
	}
	tx, err := signer.BuildSignedTx(builder, pfd)
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

// unsignedPayForData use the data in the MsgWirePayForData
// to create a new MsgPayForData.
func (msg *MsgWirePayForData) unsignedPayForData(k uint64) (*MsgPayForData, error) {
	// create the commitment using the padded message
	commit, err := CreateCommitment(k, msg.MessageNameSpaceId, msg.Message)
	if err != nil {
		return nil, err
	}

	sPFD := MsgPayForData{
		MessageNamespaceId:     msg.MessageNameSpaceId,
		MessageSize:            msg.MessageSize,
		MessageShareCommitment: commit,
		Signer:                 msg.Signer,
	}
	return &sPFD, nil
}

// ProcessWirePayForData performs the malleation process that occurs before
// creating a block. It parses the MsgWirePayForData to produce the components
// needed to create a single MsgPayForData.
func ProcessWirePayForData(msg *MsgWirePayForData, squareSize uint64) (*tmproto.Message, *MsgPayForData, []byte, error) {
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
	pfd, err := msg.unsignedPayForData(squareSize)
	if err != nil {
		return nil, nil, nil, err
	}

	return &coreMsg, pfd, shareCommit.Signature, nil
}
