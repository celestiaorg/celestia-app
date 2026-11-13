package types

import (
	"encoding/binary"
	"fmt"

	"github.com/bcp-innovations/hyperlane-cosmos/util"
)

// DeliveryMetadataBytes is the exact length of a message's delivery metadata:
// the attested root the proof is against, the leaf's index and its 32 siblings.
const DeliveryMetadataBytes = 32 + 4 + 32*util.TreeDepth

// DeliveryMetadata is what a relayer attaches to a message so the ISM can place
// it in an attested origin tree.
//
//	root(32) index(4 BE) proof(32 * 32)
//
// The relayer builds it from the tree frontier at the ISM's previous state and
// the leaves inserted since, which it indexes anyway; a proof is against one
// root, and every root ever attested stays accepted, so it never goes stale.
type DeliveryMetadata struct {
	Root  [32]byte
	Index uint32
	Proof [util.TreeDepth][32]byte
}

// DecodeDeliveryMetadata parses delivery metadata, rejecting any other length.
func DecodeDeliveryMetadata(b []byte) (*DeliveryMetadata, error) {
	if len(b) != DeliveryMetadataBytes {
		return nil, fmt.Errorf("delivery metadata must be %d bytes, got %d", DeliveryMetadataBytes, len(b))
	}
	m := &DeliveryMetadata{}
	copy(m.Root[:], b[:32])
	m.Index = binary.BigEndian.Uint32(b[32:36])
	for i := range m.Proof {
		copy(m.Proof[i][:], b[36+32*i:36+32*(i+1)])
	}
	return m, nil
}

// EncodeDeliveryMetadata is the inverse of DecodeDeliveryMetadata, for tooling
// and tests.
func EncodeDeliveryMetadata(m *DeliveryMetadata) []byte {
	out := make([]byte, 0, DeliveryMetadataBytes)
	out = append(out, m.Root[:]...)
	out = binary.BigEndian.AppendUint32(out, m.Index)
	for i := range m.Proof {
		out = append(out, m.Proof[i][:]...)
	}
	return out
}

// Proves reports whether the metadata places leaf under its root, by Hyperlane's
// own branch fold: the same keccak walk MerkleTreeHook and the EVM ISM use.
func (m *DeliveryMetadata) Proves(leaf [32]byte) bool {
	return util.BranchRoot(leaf, m.Proof, m.Index) == m.Root
}
