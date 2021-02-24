package types

import (
	"crypto/sha256"
	"errors"
	fmt "fmt"

	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/lazyledger/lazyledger-core/crypto/merkle"
	core "github.com/lazyledger/lazyledger-core/types"
	"github.com/lazyledger/nmt"
)

const (
	TypeMsgPayforMessage                   = "payformessage"
	TypeSignedTransactionDataPayForMessage = "signedtransactiondatapayformessage"
	ShareSize                              = core.ShareSize
	SquareSize                             = core.MaxSquareSize
	NamespaceIDSize                        = core.NamespaceSize
)

///////////////////////////////////////
// 	MsgWirePayForMessage
///////////////////////////////////////

var _ sdk.Msg = &MsgWirePayForMessage{}

// NewMsgWirePayForMessage creates a new MsgWirePayForMessage by using the
// namespace and message to generate share commitments for the provided square sizes
// Note that the share commitments generated still need to be signed using the Sign
// method
func NewMsgWirePayForMessage(namespace, message, pubK []byte, fee *TransactionFee, sizes ...uint64) (*MsgWirePayForMessage, error) {
	message = PadMessage(message)
	out := &MsgWirePayForMessage{
		Fee:                    fee,
		Nonce:                  0,
		MessageNameSpaceId:     namespace,
		MessageSize:            uint64(len(message)),
		Message:                message,
		MessageShareCommitment: make([]ShareCommitAndSignature, len(sizes)),
		PublicKey:              pubK,
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
// generated during the creation of the MsgWirePayForMessage
func (msg *MsgWirePayForMessage) SignShareCommitments(accName string, ring keyring.Keyring) error {
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

func (msg *MsgWirePayForMessage) Route() string { return RouterKey }

func (msg *MsgWirePayForMessage) Type() string { return TypeMsgPayforMessage }

// ValidateBasic checks for valid namespace length, declared message size, share
// commitments, signatures for those share commitments, and fulfills the sdk.Msg
// interface
func (msg *MsgWirePayForMessage) ValidateBasic() error {
	pubK := msg.PubKey()

	// ensure that the namespace id is of length == NamespaceIDSize
	if len(msg.GetMessageNameSpaceId()) != NamespaceIDSize {
		return fmt.Errorf(
			"invalid namespace length: got %d wanted %d",
			len(msg.GetMessageNameSpaceId()),
			NamespaceIDSize,
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
		calculatedCommit, err := CreateCommitment(commit.K, msg.GetMessageNameSpaceId(), msg.Message)
		if err != nil {
			return err
		}

		if string(calculatedCommit) != string(commit.ShareCommitment) {
			return fmt.Errorf("invalid commit for square size %d", commit.K)
		}

		// check that the signatures are valid
		bytesToSign, err := msg.GetCommitmentSignBytes(commit.K)
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
// message to be valid
func (msg *MsgWirePayForMessage) GetSignBytes() []byte {
	out, err := msg.GetCommitmentSignBytes(SquareSize)
	if err != nil {
		// this panic can only be reached if the nmt cannot push bytes onto the
		// tree while creating the commit. This should never happen, as an error
		// only occurs when out of order or varying sized namespaces are used,
		// and we are using an identical namespace when pushing to the nmt
		// https://github.com/lazyledger/nmt/blob/b22170d6f23796a186c07e87e4ef9856282ffd1a/nmt.go#L250
		panic(err)
	}
	return out
}

// GetSigners returns the addresses of the message signers
func (msg *MsgWirePayForMessage) GetSigners() []sdk.AccAddress {
	return []sdk.AccAddress{sdk.AccAddress(msg.PubKey().Address().Bytes())}
}

// PubKey returns the public key of the creator of MsgWirePayForMessage
func (msg *MsgWirePayForMessage) PubKey() *secp256k1.PubKey {
	return &secp256k1.PubKey{Key: msg.PublicKey}
}

// GetCommitmentSignBytes generates the bytes that each need to be signed per share commit
func (msg *MsgWirePayForMessage) GetCommitmentSignBytes(k uint64) ([]byte, error) {
	sTxMsg, err := msg.SignedTransactionDataPayForMessage(k)
	if err != nil {
		return nil, err
	}
	return sTxMsg.GetSignBytes(), nil
}

// SignedTransactionDataPayForMessage use the data in the MsgWirePayForMessage
// to create a new SignedTransactionDataPayForMessage
func (msg *MsgWirePayForMessage) SignedTransactionDataPayForMessage(k uint64) (*SignedTransactionDataPayForMessage, error) {
	// add padding to message if necessary
	msg.padMessage()

	// create the commitment using the padded message
	commit, err := CreateCommitment(k, msg.MessageNameSpaceId, msg.Message)
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

// padMessage adds padding to a message while also changing the declared message
// length
func (msg *MsgWirePayForMessage) padMessage() {
	msg.Message = PadMessage(msg.Message)

	if uint64(len(msg.Message)) != msg.MessageSize {
		msg.MessageSize = uint64(msg.MessageSize)
	}
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

// CreateCommitment generates the commit bytes for a given message, namespace, and
// squaresize using a namespace merkle tree and the rules described at
// https://github.com/lazyledger/lazyledger-specs/blob/master/rationale/message_block_layout.md#non-interactive-default-rules
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
		// create the nmt
		tree := nmt.New(sha256.New(), nmt.NamespaceIDSize(NamespaceIDSize))
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
// by the share size specified in lazyledger-core
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
