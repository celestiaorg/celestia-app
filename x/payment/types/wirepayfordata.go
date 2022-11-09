package types

import (
	"bytes"
	"errors"
	"fmt"
	"math"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	shares "github.com/celestiaorg/celestia-app/pkg/shares"
	"github.com/celestiaorg/nmt/namespace"
	sdkclient "github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	"golang.org/x/exp/constraints"
)

var _ sdk.Msg = &MsgWirePayForData{}

// NewWirePayForData creates a new MsgWirePayForData by using the namespace and
// message to generate a message share commitment. Note that the generated share
// commitment still needs to be signed using the SignShareCommitments method.
func NewWirePayForData(namespace, message []byte) (*MsgWirePayForData, error) {
	// sanity check namespace ID size
	if len(namespace) != NamespaceIDSize {
		return nil, ErrInvalidNamespaceLen.Wrapf("got: %d want: %d",
			len(namespace),
			NamespaceIDSize,
		)
	}

	out := &MsgWirePayForData{
		MessageNamespaceId:     namespace,
		MessageSize:            uint64(len(message)),
		Message:                message,
		MessageShareCommitment: &ShareCommitAndSignature{},
	}

	// generate the share commitment
	commit, err := CreateCommitment(namespace, message)
	if err != nil {
		return nil, err
	}
	out.MessageShareCommitment = &ShareCommitAndSignature{ShareCommitment: commit}
	return out, nil
}

// SignShareCommitment creates and signs the message share commitment associated
// with a MsgWirePayForData.
func (msg *MsgWirePayForData) SignShareCommitment(signer *KeyringSigner, options ...TxBuilderOption) error {
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
	// create an entire MsgPayForData and sign over it (including the signature in the commitment)
	builder := signer.NewTxBuilder(options...)

	sig, err := msg.createPayForDataSignature(signer, builder)
	if err != nil {
		return err
	}
	msg.MessageShareCommitment.Signature = sig
	return nil
}

func (msg *MsgWirePayForData) Route() string { return RouterKey }

// ValidateBasic checks for valid namespace length, declared message size, share
// commitments, signatures for those share commitments, and fulfills the sdk.Msg
// interface.
func (msg *MsgWirePayForData) ValidateBasic() error {
	if err := ValidateMessageNamespaceID(msg.GetMessageNamespaceId()); err != nil {
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
	// check that the commit is valid
	commit := msg.MessageShareCommitment
	calculatedCommit, err := CreateCommitment(msg.GetMessageNamespaceId(), msg.Message)
	if err != nil {
		return ErrCalculateCommit.Wrap(err.Error())
	}

	if !bytes.Equal(calculatedCommit, commit.ShareCommitment) {
		return ErrInvalidShareCommit
	}

	return nil
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
func (msg *MsgWirePayForData) createPayForDataSignature(signer *KeyringSigner, builder sdkclient.TxBuilder) ([]byte, error) {
	pfd, err := msg.unsignedPayForData()
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
func (msg *MsgWirePayForData) unsignedPayForData() (*MsgPayForData, error) {
	// create the commitment using the padded message
	commit, err := CreateCommitment(msg.MessageNamespaceId, msg.Message)
	if err != nil {
		return nil, err
	}

	sPFD := MsgPayForData{
		MessageNamespaceId:     msg.MessageNamespaceId,
		MessageSize:            msg.MessageSize,
		MessageShareCommitment: commit,
		Signer:                 msg.Signer,
	}
	return &sPFD, nil
}

// ProcessWirePayForData performs the malleation process that occurs before
// creating a block. It parses the MsgWirePayForData to produce the components
// needed to create a single MsgPayForData.
func ProcessWirePayForData(msg *MsgWirePayForData) (*tmproto.Message, *MsgPayForData, []byte, error) {
	// add the message to the list of core message to be returned to ll-core
	coreMsg := tmproto.Message{
		NamespaceId: msg.GetMessageNamespaceId(),
		Data:        msg.GetMessage(),
	}

	// wrap the signed transaction data
	pfd, err := msg.unsignedPayForData()
	if err != nil {
		return nil, nil, nil, err
	}

	return &coreMsg, pfd, msg.MessageShareCommitment.Signature, nil
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

// MsgMinSquareSize returns the minimum square size that msgSize can be included
// in. The returned square size does not account for the associated transaction
// shares or non-interactive defaults so it is a minimum.
func MsgMinSquareSize[T constraints.Integer](msgSize T) T {
	shareCount := shares.MsgSharesUsed(int(msgSize))
	return T(MinSquareSize(shareCount))
}

// MinSquareSize returns the minimum square size that can contain shareCount
// number of shares.
func MinSquareSize[T constraints.Integer](shareCount T) T {
	return T(shares.RoundUpPowerOfTwo(uint64(math.Ceil(math.Sqrt(float64(shareCount))))))
}
