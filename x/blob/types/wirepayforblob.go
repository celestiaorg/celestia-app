package types

import (
	"bytes"
	"errors"
	fmt "fmt"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	shares "github.com/celestiaorg/celestia-app/pkg/shares"
	"github.com/celestiaorg/nmt/namespace"
	sdkclient "github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
)

var _ sdk.Msg = &MsgWirePayForBlob{}

// NewWirePayForBlob creates a new MsgWirePayForBlob by using the
// namespace and message to generate share commitments for the provided square sizes
// Note that the share commitments generated still need to be signed using the SignShareCommitments
// method.
func NewWirePayForBlob(namespace, message []byte, sizes ...uint64) (*MsgWirePayForBlob, error) {
	// sanity check namespace ID size
	if len(namespace) != NamespaceIDSize {
		return nil, ErrInvalidNamespaceLen.Wrapf("got: %d want: %d",
			len(namespace),
			NamespaceIDSize,
		)
	}

	out := &MsgWirePayForBlob{
		NamespaceId:     namespace,
		BlobSize:        uint64(len(message)),
		Blob:            message,
		ShareCommitment: make([]ShareCommitAndSignature, len(sizes)),
	}

	// generate the share commitments
	for i, size := range sizes {
		if !shares.IsPowerOfTwo(size) {
			return nil, fmt.Errorf("invalid square size, the size must be power of 2: %d", size)
		}
		commit, err := CreateCommitment(size, namespace, message)
		if err != nil {
			return nil, err
		}
		out.ShareCommitment[i] = ShareCommitAndSignature{SquareSize: size, ShareCommitment: commit}
	}
	return out, nil
}

// SignShareCommitments creates and signs MsgPayForBlobs for each square size configured in the MsgWirePayForBlob
// to complete each shares commitment.
func (msg *MsgWirePayForBlob) SignShareCommitments(signer *KeyringSigner, options ...TxBuilderOption) error {
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
	// create an entire MsgPayForBlob and signing over it, including the signature in each commitment
	for i, commit := range msg.ShareCommitment {
		builder := signer.NewTxBuilder(options...)

		sig, err := msg.createPayForBlobSignature(signer, builder, commit.SquareSize)
		if err != nil {
			return err
		}
		msg.ShareCommitment[i].Signature = sig
	}
	return nil
}

func (msg *MsgWirePayForBlob) Route() string { return RouterKey }

// ValidateBasic checks for valid namespace length, declared message size, share
// commitments, signatures for those share commitments, and fulfills the sdk.Msg
// interface.
func (msg *MsgWirePayForBlob) ValidateBasic() error {
	if err := ValidateMessageNamespaceID(msg.GetNamespaceId()); err != nil {
		return err
	}

	if _, err := sdk.AccAddressFromBech32(msg.Signer); err != nil {
		return sdkerrors.ErrInvalidAddress.Wrapf("invalid 'from' address: %s", err)
	}

	// make sure that the message size matches the actual size of the message
	if msg.BlobSize != uint64(len(msg.Blob)) {
		return ErrDeclaredActualDataSizeMismatch.Wrapf(
			"declared: %d vs actual: %d",
			msg.BlobSize,
			len(msg.Blob),
		)
	}

	if err := msg.ValidateMessageShareCommitments(); err != nil {
		return err
	}

	return nil
}

// ValidateMessageShareCommitments returns an error if the message share
// commitments are invalid.
func (msg *MsgWirePayForBlob) ValidateMessageShareCommitments() error {
	for idx, commit := range msg.ShareCommitment {
		// check that each commit is valid
		if !shares.IsPowerOfTwo(commit.SquareSize) {
			return ErrCommittedSquareSizeNotPowOf2.Wrapf("committed to square size: %d", commit.SquareSize)
		}

		calculatedCommit, err := CreateCommitment(commit.SquareSize, msg.GetNamespaceId(), msg.Blob)
		if err != nil {
			return ErrCalculateCommit.Wrap(err.Error())
		}

		if !bytes.Equal(calculatedCommit, commit.ShareCommitment) {
			return ErrInvalidShareCommit.Wrapf("for square size %d and commit number %v", commit.SquareSize, idx)
		}
	}

	if len(msg.ShareCommitment) == 0 {
		return ErrNoMessageShareCommitments
	}

	if err := msg.ValidateAllSquareSizesCommitedTo(); err != nil {
		return err
	}

	return nil
}

// ValidateAllSquareSizesCommitedTo returns an error if the list of square sizes
// committed to don't match all square sizes expected for this message size.
func (msg *MsgWirePayForBlob) ValidateAllSquareSizesCommitedTo() error {
	allSquareSizes := AllSquareSizes(int(msg.BlobSize))
	committedSquareSizes := msg.committedSquareSizes()

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
func (msg *MsgWirePayForBlob) committedSquareSizes() []uint64 {
	squareSizes := make([]uint64, 0, len(msg.ShareCommitment))
	for _, commit := range msg.ShareCommitment {
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
func (msg *MsgWirePayForBlob) GetSigners() []sdk.AccAddress {
	address, err := sdk.AccAddressFromBech32(msg.Signer)
	if err != nil {
		panic(err)
	}
	return []sdk.AccAddress{address}
}

// createPayForBlobSignature generates the signature for a PayForBlob for a
// single squareSize using the info from a MsgWirePayForBlob.
func (msg *MsgWirePayForBlob) createPayForBlobSignature(signer *KeyringSigner, builder sdkclient.TxBuilder, squareSize uint64) ([]byte, error) {
	pfb, err := msg.unsignedPayForBlob(squareSize)
	if err != nil {
		return nil, err
	}
	tx, err := signer.BuildSignedTx(builder, pfb)
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

// unsignedPayForBlob use the data in the MsgWirePayForBlob
// to create a new MsgPayForBlob.
func (msg *MsgWirePayForBlob) unsignedPayForBlob(squareSize uint64) (*MsgPayForBlob, error) {
	// create the commitment using the padded message
	commit, err := CreateCommitment(squareSize, msg.NamespaceId, msg.Blob)
	if err != nil {
		return nil, err
	}

	sPFB := MsgPayForBlob{
		NamespaceId:     msg.NamespaceId,
		BlobSize:        msg.BlobSize,
		ShareCommitment: commit,
		Signer:          msg.Signer,
	}
	return &sPFB, nil
}

// ProcessWirePayForBlob performs the malleation process that occurs before
// creating a block. It parses the MsgWirePayForBlob to produce the components
// needed to create a single MsgPayForBlob.
func ProcessWirePayForBlob(msg *MsgWirePayForBlob, squareSize uint64) (*tmproto.Message, *MsgPayForBlob, []byte, error) {
	// make sure that a ShareCommitAndSignature of the correct size is
	// included in the message
	var shareCommit ShareCommitAndSignature
	for _, commit := range msg.ShareCommitment {
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
		NamespaceId: msg.GetNamespaceId(),
		Data:        msg.GetBlob(),
	}

	// wrap the signed transaction data
	pfb, err := msg.unsignedPayForBlob(squareSize)
	if err != nil {
		return nil, nil, nil, err
	}

	return &coreMsg, pfb, shareCommit.Signature, nil
}

// HasWirePayForBlob performs a quick but not definitive check to see if a tx
// contains a MsgWirePayForBlob. The check is quick but not definitive because
// it only uses a proto.Message generated method instead of performing a full
// type check.
func HasWirePayForBlob(tx sdk.Tx) bool {
	for _, msg := range tx.GetMsgs() {
		msgName := sdk.MsgTypeURL(msg)
		if msgName == URLMsgWirePayForBlob {
			return true
		}
	}
	return false
}

// ExtractMsgWirePayForBlob attempts to extract a MsgWirePayForBlob from a
// provided sdk.Tx. It returns an error if no MsgWirePayForBlob is found.
func ExtractMsgWirePayForBlob(tx sdk.Tx) (*MsgWirePayForBlob, error) {
	noWirePFBError := errors.New("sdk.Tx does not contain MsgWirePayForBlob sdk.Msg")
	// perform a quick check before attempting a type check
	if !HasWirePayForBlob(tx) {
		return nil, noWirePFBError
	}

	// only support malleated transactions that contain a single sdk.Msg
	if len(tx.GetMsgs()) != 1 {
		return nil, errors.New("sdk.Txs with a single MsgWirePayForBlob are currently supported")
	}

	msg := tx.GetMsgs()[0]
	wireMsg, ok := msg.(*MsgWirePayForBlob)
	if !ok {
		return nil, noWirePFBError
	}

	return wireMsg, nil
}
