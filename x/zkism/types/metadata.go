package types

import (
	"encoding/binary"
	"fmt"

	"github.com/bcp-innovations/hyperlane-cosmos/util"
)

// MerkleProofMetadata is the metadata provided by relayers when delivering a message. It proves inclusion
// of the message id in the source chain merkle tree.
type MerkleProofMetadata struct {
	// Index is the leaf index of the message in the merkle tree.
	Index uint32
	// Branch is the merkle branch from the leaf to the root.
	Branch [util.TreeDepth][32]byte
}

// ParseMerkleProofMetadata decodes metadata encoded as: index (uint32, big-endian) || branch (TreeDepth * 32 bytes).
func ParseMerkleProofMetadata(metadata []byte) (MerkleProofMetadata, error) {
	if len(metadata) != MerkleProofMetadataBytes {
		return MerkleProofMetadata{}, fmt.Errorf("invalid metadata length: expected %d, got %d", MerkleProofMetadataBytes, len(metadata))
	}

	var m MerkleProofMetadata
	m.Index = binary.BigEndian.Uint32(metadata[:4])
	for i := range m.Branch {
		copy(m.Branch[i][:], metadata[4+i*32:4+(i+1)*32])
	}

	return m, nil
}
