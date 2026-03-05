package fibre

import (
	"bytes"
	"encoding/hex"
	"fmt"

	"github.com/celestiaorg/celestia-app/v8/pkg/rsema1d"
)

// CommitmentSize is the size of a Commitment in bytes.
const CommitmentSize = 32

// Commitment is a 32-byte blob commitment used for protocol messages and storage indexing.
// TODO: merge with rsema1d.Commitment once that package stabilizes.
type Commitment rsema1d.Commitment

// String returns the hex-encoded string representation.
func (c Commitment) String() string {
	return hex.EncodeToString(c[:])
}

// UnmarshalBinary decodes a Commitment from bytes.
func (c *Commitment) UnmarshalBinary(data []byte) error {
	if len(data) != CommitmentSize {
		return fmt.Errorf("commitment must be %d bytes, got %d", CommitmentSize, len(data))
	}
	copy(c[:], data)
	return nil
}

// CommitmentFromString decodes a Commitment from a hex-encoded string.
func CommitmentFromString(s string) (Commitment, error) {
	data, err := hex.DecodeString(s)
	if err != nil {
		return Commitment{}, fmt.Errorf("decoding hex: %w", err)
	}
	if len(data) != CommitmentSize {
		return Commitment{}, fmt.Errorf("commitment must be %d bytes, got %d", CommitmentSize, len(data))
	}
	var c Commitment
	copy(c[:], data)
	return c, nil
}

// BlobIDSize is the size of a BlobID in bytes.
// Format: [version (1 byte) | Commitment (32 bytes)]
const BlobIDSize = 33

// BlobID uniquely identifies a blob by combining version and commitment.
// The first byte encodes the blob version, followed by 32 bytes of commitment.
// This makes BlobIDs self-describing, allowing clients to know the blob format before downloading.
type BlobID []byte

// NewBlobID creates a BlobID from version and commitment.
func NewBlobID(version uint8, commitment Commitment) BlobID {
	id := make(BlobID, BlobIDSize)
	id[0] = version
	copy(id[1:], commitment[:])
	return id
}

// Version returns the blob version encoded in this BlobID.
// Panics if the BlobID is invalid (empty or wrong length).
func (id BlobID) Version() uint8 {
	return id[0]
}

// Validate returns an error if the BlobID is invalid.
func (id BlobID) Validate() error {
	if len(id) != BlobIDSize {
		return fmt.Errorf("blob ID must be %d bytes, got %d", BlobIDSize, len(id))
	}
	return nil
}

// Commitment returns the commitment (without version prefix).
func (id BlobID) Commitment() Commitment {
	var c Commitment
	copy(c[:], id[1:])
	return c
}

// UnmarshalBinary decodes a [BlobID] from bytes.
func (id *BlobID) UnmarshalBinary(data []byte) error {
	if len(data) != BlobIDSize {
		return fmt.Errorf("blob ID must be %d bytes, got %d", BlobIDSize, len(data))
	}
	*id = make(BlobID, BlobIDSize)
	copy(*id, data)
	return nil
}

// String returns the hex-encoded string representation of the BlobID.
func (id BlobID) String() string {
	return hex.EncodeToString(id)
}

// BlobIDFromString decodes a [BlobID] from a hex-encoded string.
func BlobIDFromString(s string) (BlobID, error) {
	data, err := hex.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("decoding hex: %w", err)
	}
	var id BlobID
	if err := id.UnmarshalBinary(data); err != nil {
		return nil, err
	}
	return id, nil
}

// Equals returns true if the two BlobIDs are equal.
func (id BlobID) Equals(other BlobID) bool {
	return bytes.Equal(id, other)
}
