package types

import (
	"bytes"
	"errors"
	fmt "fmt"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/nmt/namespace"
	sdkclient "github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
)

var _ sdk.Msg = &MsgWirePayForData{}

// NewWirePayForData creates a new MsgWirePayForData by using the
// namespace and message to generate share commitments for the provided square sizes
// Note that the share commitments generated still need to be signed using the SignShareCommitments
// method.
func NewWirePayForData(namespace, message []byte, sizes ...uint64) (*MsgWirePayForData, error) {
	// sanity check namespace ID size
	if len(namespace) != NamespaceIDSize {
		return nil, ErrInvalidNamespaceLen.Wrapf("got: %d want: %d",
			len(namespace),
			NamespaceIDSize,
		)
	}

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
		out.MessageShareCommitment[i] = ShareCommitAndSignature{SquareSize: size, ShareCommitment: commit}
	}
	return out, nil
}

// SignShareCommitments creates and signs MsgPayForDatas for each square size configured in the MsgWirePayForData
// to complete each shares commitment.
func (msg *MsgWirePayForData) SignShareCommitments(signer *KeyringSigner, options ...TxBuilderOption) error {
	addr, err := signer.GetSignerInfo().GetAddress()
	if err != nil {
		return err
	}

	if addr == nil {
		return errors.New("failed to get address")
	}
	if addr.Empty() {
		return errors.New("failed to get address")
	}

	msg.Signer = addr.String()
	// create an entire MsgPayForData and signing over it, including the signature in each commitment
	for i, commit := range msg.MessageShareCommitment {
		builder := signer.NewTxBuilder(options...)

		sig, err := msg.createPayForDataSignature(signer, builder, commit.SquareSize)
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
	if err := ValidateMessageNamespaceID(msg.GetMessageNameSpaceId()); err != nil {
		return err
	}

	if _, err := sdk.AccAddressFromBech32(msg.Signer); err != nil {
		return sdkerrors.ErrInvalidAddress.Wrapf("invalid 'from' address: %s", err)
	}

	// make sure that the message size matches the actual size of the message
	if msg.MessageSize != uint64(len(msg.Message)) {
		return ErrDeclaredActualDataSizeMismatch.Wrapf(
			"declared: %d vs actual: %d",
			msg.MessageSize,
			len(msg.Message),
		)
	}

	if err := msg.ValidateMessageShareCommitments(); err != nil {
		return err
	}

	return nil
}

// ValidateMessageShareCommitments returns an error if the message share
// commitments are invalid.
func (msg *MsgWirePayForData) ValidateMessageShareCommitments() error {
	for idx, commit := range msg.MessageShareCommitment {
		// check that each commit is valid
		if !powerOf2(commit.SquareSize) {
			return ErrCommittedSquareSizeNotPowOf2.Wrapf("committed to square size: %d", commit.SquareSize)
		}

		calculatedCommit, err := CreateCommitment(commit.SquareSize, msg.GetMessageNameSpaceId(), msg.Message)
		if err != nil {
			return ErrCalculateCommit.Wrap(err.Error())
		}

		if !bytes.Equal(calculatedCommit, commit.ShareCommitment) {
			return ErrInvalidShareCommit.Wrapf("for square size %d and commit number %v", commit.SquareSize, idx)
		}
	}

	if len(msg.MessageShareCommitment) == 0 {
		return ErrNoMessageShareCommitments
	}

	if err := msg.ValidateAllSquareSizesCommitedTo(); err != nil {
		return err
	}

	return nil
}

// ValidateAllSquareSizesCommitedTo returns an error if the list of square sizes
// committed to don't match all square sizes expected for this message size.
func (msg *MsgWirePayForData) ValidateAllSquareSizesCommitedTo() error {
	allSquareSizes := AllSquareSizes(int(msg.MessageSize))
	committedSquareSizes := msg.committedSquareSizes()

	if len(allSquareSizes) != len(committedSquareSizes) {
		return ErrInvalidShareCommitments.Wrapf("length of all square sizes: %v must equal length of committed square sizes: %v", len(allSquareSizes), len(committedSquareSizes))
	}

	if !isEqual(allSquareSizes, committedSquareSizes) {
		return ErrInvalidShareCommitments.Wrapf("all square sizes: %v, committed square sizes: %v", allSquareSizes, committedSquareSizes)
	}
	return nil
}

// isEqual returns true if the given uint64 slices are equal
func isEqual(a, b []uint64) bool {
	if len(a) != len(b) {
		return false
	}
	for i, v := range a {
		if v != b[i] {
			return false
		}
	}
	return true
}

// commitedSquareSizes returns a list of square sizes that are present in a
// message's share commitment.
func (msg *MsgWirePayForData) committedSquareSizes() []uint64 {
	squareSizes := make([]uint64, 0, len(msg.MessageShareCommitment))
	for _, commit := range msg.MessageShareCommitment {
		squareSizes = append(squareSizes, commit.SquareSize)
	}
	return squareSizes
}

// ValidateMessageNamespaceID returns an error if the provided namespace.ID is an invalid or reserved namespace id.
func ValidateMessageNamespaceID(ns namespace.ID) error {
	// ensure that the namespace id is of length == NamespaceIDSize
	if nsLen := len(ns); nsLen != NamespaceIDSize {
		return ErrInvalidNamespaceLen.Wrapf("got: %d want: %d",
			nsLen,
			NamespaceIDSize,
		)
	}
	// ensure that a reserved namespace is not used
	if bytes.Compare(ns, appconsts.MaxReservedNamespace) < 1 {
		return ErrReservedNamespace.Wrapf("got namespace: %x, want: > %x", ns, appconsts.MaxReservedNamespace)
	}

	// ensure that ParitySharesNamespaceID is not used
	if bytes.Equal(ns, appconsts.ParitySharesNamespaceID) {
		return ErrParitySharesNamespace
	}

	// ensure that TailPaddingNamespaceID is not used
	if bytes.Equal(ns, appconsts.TailPaddingNamespaceID) {
		return ErrTailPaddingNamespace
	}

	return nil
}

// GetSigners returns the addresses of the message signers
func (msg *MsgWirePayForData) GetSigners() []sdk.AccAddress {
	address, err := sdk.AccAddressFromBech32(msg.Signer)
	if err != nil {
		panic(err)
	}
	return []sdk.AccAddress{address}
}

// createPayForDataSignature generates the signature for a PayForData for a
// single squareSize using the info from a MsgWirePayForData.
func (msg *MsgWirePayForData) createPayForDataSignature(signer *KeyringSigner, builder sdkclient.TxBuilder, squareSize uint64) ([]byte, error) {
	pfd, err := msg.unsignedPayForData(squareSize)
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
func (msg *MsgWirePayForData) unsignedPayForData(squareSize uint64) (*MsgPayForData, error) {
	// create the commitment using the padded message
	commit, err := CreateCommitment(squareSize, msg.MessageNameSpaceId, msg.Message)
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
	var shareCommit ShareCommitAndSignature
	for _, commit := range msg.MessageShareCommitment {
		if commit.SquareSize == squareSize {
			shareCommit = commit
			break
		}
	}
	if shareCommit.Signature == nil {
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

// HasWirePayForData performs a quick but not definitive check to see if a tx
// contains a MsgWirePayForData. The check is quick but not definitive because
// it only uses a proto.Message generated method instead of performing a full
// type check.
func HasWirePayForData(tx sdk.Tx) bool {
	for _, msg := range tx.GetMsgs() {
		msgName := sdk.MsgTypeURL(msg)
		if msgName == URLMsgWirePayForData {
			return true
		}
	}
	return false
}

// ExtractMsgWirePayForData attempts to extract a MsgWirePayForData from a
// provided sdk.Tx. It returns an error if no MsgWirePayForData is found.
func ExtractMsgWirePayForData(tx sdk.Tx) (*MsgWirePayForData, error) {
	noWirePFDError := errors.New("sdk.Tx does not contain MsgWirePayForData sdk.Msg")
	// perform a quick check before attempting a type check
	if !HasWirePayForData(tx) {
		return nil, noWirePFDError
	}

	// only support malleated transactions that contain a single sdk.Msg
	if len(tx.GetMsgs()) != 1 {
		return nil, errors.New("sdk.Txs with a single MsgWirePayForData are currently supported")
	}

	msg := tx.GetMsgs()[0]
	wireMsg, ok := msg.(*MsgWirePayForData)
	if !ok {
		return nil, noWirePFDError
	}

	return wireMsg, nil
}
