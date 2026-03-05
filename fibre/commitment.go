package fibre

import (
	"encoding/hex"
	"fmt"
)

// CommitmentSize is the size of a Commitment in bytes.
const CommitmentSize = 32

// Commitment is a 32-byte blob commitment used for protocol messages and storage indexing.
type Commitment [CommitmentSize]byte

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
