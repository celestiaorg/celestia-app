package types

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
)

// StateTransitionValues are the set of proof public values used when verifying state transition proofs.
type StateTransitionValues struct {
	// The (trusted) state stored in the ISM
	State []byte
	// The new (trusted) state after the state transition
	NewState []byte
}

// Marshal encodes the PublicValues struct into a bincode-compatible byte slice.
// The output format uses Rust bincode's default configuration: (little-endian, fixed-width integers, length-prefixed slices).
func (v *StateTransitionValues) Marshal() ([]byte, error) {
	var buf bytes.Buffer

	// write length of State
	l := uint64(len(v.State))
	if err := binary.Write(&buf, binary.LittleEndian, l); err != nil {
		return nil, err
	}

	// write State
	if _, err := buf.Write(v.State); err != nil {
		return nil, err
	}

	// write length of NewState
	newL := uint64(len(v.NewState))
	if err := binary.Write(&buf, binary.LittleEndian, newL); err != nil {
		return nil, err
	}

	// write NewState
	if _, err := buf.Write(v.NewState); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// Unmarshal decodes a bincode-serialized PublicValues struct.
// This function expects the input byte slice to be encoded using Rust bincode's
// default configuration: (little-endian, fixed-width integers, length-prefixed slices).
func (v *StateTransitionValues) Unmarshal(data []byte) error {
	buf := bytes.NewReader(data)

	// read length of State
	var l uint64
	if err := binary.Read(buf, binary.LittleEndian, &l); err != nil {
		return err
	}

	if l < MinStateBytes || l > MaxStateBytes {
		return fmt.Errorf("invalid state length, must be between %d and %d", MinStateBytes, MaxStateBytes)
	}

	// read State
	v.State = make([]byte, l)
	if _, err := io.ReadFull(buf, v.State); err != nil {
		return err
	}

	// read length of NewState
	var newL uint64
	if err := binary.Read(buf, binary.LittleEndian, &newL); err != nil {
		return err
	}

	if newL < MinStateBytes || newL > MaxStateBytes {
		return fmt.Errorf("invalid state length, must be between %d and %d", MinStateBytes, MaxStateBytes)
	}

	// read NewState
	v.NewState = make([]byte, newL)
	if _, err := io.ReadFull(buf, v.NewState); err != nil {
		return err
	}

	return nil
}

// StateMembershipValues are the set of proof public values used when verifying state membership inclusion of
// Hyperlane messages.
type StateMembershipValues struct {
	StateRoot         [32]byte
	MerkleTreeAddress [32]byte
	MessageIds        [][32]byte
}

// Marshal encodes the EvHyperlanePublicValues struct into a bincode-compatible byte slice.
// The output format uses Rust bincode's default configuration: (little-endian, fixed-width integers, length-prefixed slices).
func (v *StateMembershipValues) Marshal() ([]byte, error) {
	var buf bytes.Buffer

	if err := writeBytes(&buf, v.StateRoot[:]); err != nil {
		return nil, fmt.Errorf("write StateRoot: %w", err)
	}

	if err := writeBytes(&buf, v.MerkleTreeAddress[:]); err != nil {
		return nil, fmt.Errorf("write MerkleTreeAddress: %w", err)
	}

	count := uint64(len(v.MessageIds))
	if err := binary.Write(&buf, binary.LittleEndian, count); err != nil {
		return nil, fmt.Errorf("write MessageIds length: %w", err)
	}

	for i, id := range v.MessageIds {
		if err := writeBytes(&buf, id[:]); err != nil {
			return nil, fmt.Errorf("write MessageIds[%d]: %w", i, err)
		}
	}

	return buf.Bytes(), nil
}

// Unmarshal decodes a bincode-serialized EvHyperlanePublicValues struct.
// This function expects the input byte slice to be encoded using Rust bincode's
// default configuration: (little-endian, fixed-width integers, length-prefixed slices).
func (v *StateMembershipValues) Unmarshal(data []byte) error {
	buf := bytes.NewReader(data)

	if _, err := buf.Read(v.StateRoot[:]); err != nil {
		return fmt.Errorf("read StateRoot: %w", err)
	}

	if _, err := buf.Read(v.MerkleTreeAddress[:]); err != nil {
		return fmt.Errorf("read MerkleTreeAddress: %w", err)
	}

	var count uint64 // read uint64 (little-endian) length prefix
	if err := binary.Read(buf, binary.LittleEndian, &count); err != nil {
		return fmt.Errorf("read message ids length: %w", err)
	}

	if count > MaxMessageIdsCount {
		return fmt.Errorf("message ids count %d exceeds maximum allowed %d", count, MaxMessageIdsCount)
	}

	remaining := buf.Len()
	if remaining < int(count*32) {
		return fmt.Errorf("buffer too short: need %d, have %d", count*32, remaining)
	}

	v.MessageIds = make([][32]byte, count)
	for i := 0; i < int(count); i++ {
		if _, err := buf.Read(v.MessageIds[i][:]); err != nil {
			return fmt.Errorf("read message_id %d: %w", i, err)
		}
	}

	return nil
}

func writeBytes(w io.Writer, b []byte) error {
	_, err := w.Write(b)
	return err
}
