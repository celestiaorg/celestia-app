package types

import (
	"crypto/sha256"
	"errors"
	fmt "fmt"

	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/lazyledger/lazyledger-core/crypto/merkle"
	"github.com/lazyledger/nmt"
)

const (
	TypeMsgPayforMessage                   = "payformessage"
	TypeSignedTransactionDataPayForMessage = "signedtransactiondatapayformessage"
	ShareSize                              = 256
)

///////////////////////////////////////
// 	MsgWirePayForMessage
///////////////////////////////////////

var _ sdk.MsgRequest = &MsgWirePayForMessage{}

func (msg *MsgWirePayForMessage) Route() string { return RouterKey }

func (msg *MsgWirePayForMessage) Type() string { return TypeMsgPayforMessage }

// ValidateBasic checks for valid namespace length, declared message size, share
// commitments, signatures for those share commitments, and fulfills the sdk.Msg
// interface
func (msg *MsgWirePayForMessage) ValidateBasic() error {
	pubK := msg.PubKey()

	// ensure that the namespace id is of length == 8
	if len(msg.GetMessageNameSpaceId()) != 8 {
		return fmt.Errorf(
			"invalid namespace length: got %d wanted 8",
			len(msg.GetMessageNameSpaceId()),
		)
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

	for _, commit := range msg.MessageShareCommitment {
		// check that each commit is valid
		calculatedCommit, err := CreateCommit(commit.K, msg.GetMessageNameSpaceId(), msg.Message)
		if err != nil {
			return err
		}

		if string(calculatedCommit) != string(commit.ShareCommitment) {
			return fmt.Errorf("invalid commit for square size %d", commit.K)
		}

		// check that the signatures are valid
		bytesToSign, err := msg.GetCommitSignBytes(commit.K)
		if err != nil {
			return err
		}

		if !pubK.VerifySignature(bytesToSign, commit.Signature) {
			return fmt.Errorf("invalid signature for share commitment to square size %d", commit.K)
		}
	}

	return nil
}

// GetSignBytes returns messages bytes that need to be signed in order for the
// message to be valid todo(evan): follow the spec so that each share commitment
// is signed, instead of the entire message
// TODO(evan): remove panic and add possibility to vary square size
func (msg *MsgWirePayForMessage) GetSignBytes() []byte {
	out, err := msg.GetCommitSignBytes(64)
	if err != nil {
		panic(err)
	}
	return out
}

// GetSigners returns the addresses of the message signers
func (msg *MsgWirePayForMessage) GetSigners() []sdk.AccAddress {
	return []sdk.AccAddress{sdk.AccAddress(msg.PubKey().Address().Bytes())}
}

// PubKey uses the string version of the public key to
func (msg *MsgWirePayForMessage) PubKey() *secp256k1.PubKey {
	return &secp256k1.PubKey{Key: msg.PublicKey}
}

// GetCommitSignBytes generates the bytes that each need to be signed per share commit
func (msg *MsgWirePayForMessage) GetCommitSignBytes(k uint64) ([]byte, error) {
	sTxMsg, err := msg.SignedTransactionDataPayForMessage(k)
	if err != nil {
		return nil, err
	}
	return sTxMsg.GetSignBytes(), nil
}

// SignedTransactionDataPayForMessage use the data in the MsgWirePayForMessage
// to create a new SignedTransactionDataPayForMessage
func (msg *MsgWirePayForMessage) SignedTransactionDataPayForMessage(k uint64) (*SignedTransactionDataPayForMessage, error) {
	commit, err := CreateCommit(k, msg.MessageNameSpaceId, msg.Message)
	if err != nil {
		return nil, err
	}
	sTxMsg := SignedTransactionDataPayForMessage{
		Fee: &TransactionFee{
			BaseRateMax: msg.Fee.BaseRateMax,
			TipRateMax:  msg.Fee.TipRateMax,
		},
		Nonce:                  msg.Nonce,
		MessageNamespaceId:     msg.MessageNameSpaceId,
		MessageSize:            msg.MessageSize,
		MessageShareCommitment: commit,
	}
	return &sTxMsg, nil
}

///////////////////////////////////////
// 	SignedTransactionDataPayForMessage
///////////////////////////////////////

var _ sdk.Tx = &TxSignedTransactionDataPayForMessage{}

// GetMsgs fullfills the sdk.Tx interface
func (tx *TxSignedTransactionDataPayForMessage) GetMsgs() []sdk.Msg {
	return []sdk.Msg{tx.Message}
}

// ValidateBasic fullfills the sdk.Tx interface by verifing the signature of the
// underlying signed transaction
func (tx *TxSignedTransactionDataPayForMessage) ValidateBasic() error {
	pKey := secp256k1.PubKey{Key: tx.PublicKey}

	if !pKey.VerifySignature(tx.Message.GetSignBytes(), tx.Signature) {
		return errors.New("failure to validte SignedTransactionDataPayForMessage")
	}
	return nil
}

var _ sdk.Msg = &SignedTransactionDataPayForMessage{}

// Route fullfills the sdk.Msg interface
func (msg *SignedTransactionDataPayForMessage) Route() string { return RouterKey }

// Type fullfills the sdk.Msg interface
func (msg *SignedTransactionDataPayForMessage) Type() string {
	return TypeSignedTransactionDataPayForMessage
}

// ValidateBasic fullfills the sdk.Msg interface by performing stateless
// validity checks on the msg that also don't require having the actual message
func (msg *SignedTransactionDataPayForMessage) ValidateBasic() error {
	// ensure that the namespace id is of length == 8
	if len(msg.GetMessageNamespaceId()) != 8 {
		return fmt.Errorf(
			"invalid namespace length: got %d wanted 8",
			len(msg.GetMessageNamespaceId()),
		)
	}
	return nil
}

// GetSignBytes fullfills the sdk.Msg interface by reterning a deterministic set
// of bytes to sign over
func (msg *SignedTransactionDataPayForMessage) GetSignBytes() []byte {
	return sdk.MustSortJSON(ModuleCdc.MustMarshalJSON(msg))
}

// GetSigners fullfills the sdk.Msg interface but does not return anything, as
// SignTransactionDataPayForMessage doesn't have access the public key necessary
// in MsgWirePayForMessage
func (msg *SignedTransactionDataPayForMessage) GetSigners() []sdk.AccAddress {
	return []sdk.AccAddress{}
}

///////////////////////////////////////
// 	Utilities
///////////////////////////////////////

// CreateCommit generates the commit bytes for a given message, namespace, and
// squaresize using a namespace merkle tree and the rules described at
// https://github.com/lazyledger/lazyledger-specs/blob/master/rationale/message_block_layout.md#non-interactive-default-rules
func CreateCommit(k uint64, namespace, message []byte) ([]byte, error) {
	// break message into shares
	shares := ChunkMessage(message)

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
		// create the nmt
		tree := nmt.New(sha256.New(), nmt.NamespaceIDSize(8))
		for _, leaf := range set {
			err := tree.Push(namespace, leaf)
			if err != nil {
				return nil, err
			}
		}
		// add the root
		subTreeRoots[i] = tree.Root().Bytes()
	}
	return merkle.HashFromByteSlices(subTreeRoots), nil
}

// ChunkMessage breaks the message into 256 byte pieces
func ChunkMessage(message []byte) [][]byte {
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
