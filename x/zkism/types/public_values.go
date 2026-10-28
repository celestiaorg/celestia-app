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

// StateMembershipValues are the set of proof public values used when verifying state membership of the
// source chain Hyperlane merkle tree. The encoded layout is
// version (u8) || state_root (32) || merkle_tree_address (32) || tree_root (32) || count (u32 LE).
type StateMembershipValues struct {
	StateRoot         [32]byte
	MerkleTreeAddress [32]byte
	// TreeRoot is the root of the merkle tree at MerkleTreeAddress in StateRoot.
	TreeRoot [32]byte
	// Count is the number of leaves inserted into the merkle tree.
	Count uint32
}

// Marshal encodes the StateMembershipValues struct into a bincode-compatible byte slice.
// The output format uses Rust bincode's default configuration: (little-endian, fixed-width integers).
// The layout version is always StateMembershipValuesVersion.
func (v *StateMembershipValues) Marshal() ([]byte, error) {
	bz := make([]byte, 0, StateMembershipValuesBytes)
	bz = append(bz, StateMembershipValuesVersion)
	bz = append(bz, v.StateRoot[:]...)
	bz = append(bz, v.MerkleTreeAddress[:]...)
	bz = append(bz, v.TreeRoot[:]...)
	bz = binary.LittleEndian.AppendUint32(bz, v.Count)

	return bz, nil
}

// Unmarshal decodes a bincode-serialized StateMembershipValues struct.
// This function expects the input byte slice to be encoded using Rust bincode's
// default configuration: (little-endian, fixed-width integers).
// Payloads of any other length or layout version are rejected.
func (v *StateMembershipValues) Unmarshal(data []byte) error {
	if len(data) != StateMembershipValuesBytes {
		return fmt.Errorf("invalid state membership values length: expected %d, got %d", StateMembershipValuesBytes, len(data))
	}

	if data[0] != StateMembershipValuesVersion {
		return fmt.Errorf("unsupported state membership values version: expected %d, got %d", StateMembershipValuesVersion, data[0])
	}

	copy(v.StateRoot[:], data[1:33])
	copy(v.MerkleTreeAddress[:], data[33:65])
	copy(v.TreeRoot[:], data[65:97])
	v.Count = binary.LittleEndian.Uint32(data[97:101])

	return nil
}
