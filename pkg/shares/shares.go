package shares

import (
	"encoding/binary"

	"github.com/celestiaorg/nmt/namespace"
	coretypes "github.com/tendermint/tendermint/types"
)

// Share contains the raw share data (including namespace ID).
type Share []byte

// NamespacedShare extends a Share with the corresponding namespace.
type NamespacedShare struct {
	Share
	ID namespace.ID
}

func (n NamespacedShare) NamespaceID() namespace.ID {
	return n.ID
}

func (n NamespacedShare) Data() []byte {
	return n.Share
}

// NamespacedShares is just a list of NamespacedShare elements.
// It can be used to extract the raw shares.
type NamespacedShares []NamespacedShare

// RawShares returns the raw shares that can be fed into the erasure coding
// library (e.g. rsmt2d).
func (ns NamespacedShares) RawShares() [][]byte {
	res := make([][]byte, len(ns))
	for i, nsh := range ns {
		res[i] = nsh.Share
	}
	return res
}

// MarshalDelimitedTx prefixes a transaction with the length of the transaction
// encoded as a varint.
func MarshalDelimitedTx(tx coretypes.Tx) ([]byte, error) {
	lenBuf := make([]byte, binary.MaxVarintLen64)
	length := uint64(len(tx))
	n := binary.PutUvarint(lenBuf, length)
	return append(lenBuf[:n], tx...), nil
}

// MarshalDelimitedMessage marshals the raw share data (excluding the namespace)
// of this message and prefixes it with the length of the message encoded as a
// varint.
func MarshalDelimitedMessage(msg coretypes.Message) ([]byte, error) {
	lenBuf := make([]byte, binary.MaxVarintLen64)
	length := uint64(len(msg.Data))
	n := binary.PutUvarint(lenBuf, length)
	return append(lenBuf[:n], msg.Data...), nil
}
