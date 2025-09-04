package types

import (
	"encoding/binary"
	"errors"
	"fmt"
)

type ProofType uint8

const (
	ProofTypeSP1Groth16 ProofType = iota
)

// ZkExecutionISMMetadata contains the ZK proof and verification data.
type ZkExecutionISMMetadata struct {
	// ProofType is the type of ZK proof system used. Default: Groth16.
	ProofType ProofType
	// Proof is the ZK proof bytes.
	Proof []byte
	// PublicValues defines the public values used for proof verification.
	PublicValues PublicValues
	// MerkleProofs defines a set of merkle proofs used for proving inclusion of a message - TBD.
	MerkleProofs [][]byte
}

// NewZkExecutionISMMetadata parses a raw metadata byte slice into a structured format.
// The ZK Execution ISM metadata follows the following format:
// [0]          - Type of the ZK Proof System used (e.g. Groth16)
// [1:5]        - Size of the ZK Proof, N, if it exists
// [5:N+5]      - The ZK proof
// [N+5:N+9]    - Size of public values, M
// [N+9:N+9+M]  - Public values serialized using Rust bincode default format.
// [N+9+M:]     - Merkle Proofs
func NewZkExecutionISMMetadata(metadata []byte) (ZkExecutionISMMetadata, error) {
	if len(metadata) < 5 {
		return ZkExecutionISMMetadata{}, errors.New("metadata too short to contain proof type and size")
	}

	offset := 0

	// [0] - Type of the ZK Proof System used
	proofType := ProofType(metadata[offset])
	if proofType != ProofTypeSP1Groth16 {
		return ZkExecutionISMMetadata{}, fmt.Errorf("invalid proof type: expected %d, got %d", ProofTypeSP1Groth16, proofType)
	}

	offset++

	// [1:5] - Size of the proof, N (uint32)
	proofSize := binary.BigEndian.Uint32(metadata[offset : offset+4])
	offset += 4
	if len(metadata[offset:]) < int(proofSize) {
		return ZkExecutionISMMetadata{}, fmt.Errorf("metadata too short to contain full proof: expected %d bytes", proofSize)
	}

	// [5:N+5] - ZK proof
	proof := metadata[offset : offset+int(proofSize)]
	offset += int(proofSize)

	// [N+5:N+9] - Size of public values, M (uint32)
	if len(metadata[offset:]) < 4 {
		return ZkExecutionISMMetadata{}, errors.New("metadata too short to contain number of public values")
	}

	pubInputsSize := binary.BigEndian.Uint32(metadata[offset : offset+4])
	offset += 4

	if len(metadata[offset:]) < int(pubInputsSize) {
		return ZkExecutionISMMetadata{}, fmt.Errorf("metadata too short to contain public values: expected %d bytes", pubInputsSize)
	}

	var publicInputs PublicValues
	if pubInputsSize != 0 {
		// [N+9:N+9+M] - bincode-encoded PublicInputs
		pubInputsBz := metadata[offset : offset+int(pubInputsSize)]
		offset += int(pubInputsSize)

		if err := publicInputs.Unmarshal(pubInputsBz); err != nil {
			return ZkExecutionISMMetadata{}, fmt.Errorf("failed to decode PublicInputs: %w", err)
		}
	}

	// [remainder] - merkle proofs (assume 32 bytes each)
	merkleProofs := make([][]byte, 0)
	for len(metadata[offset:]) >= 32 {
		merkleProofs = append(merkleProofs, metadata[offset:offset+32])
		offset += 32
	}

	// if there's leftover data less than 32 bytes, it's likely malformed
	if len(metadata[offset:]) > 0 {
		return ZkExecutionISMMetadata{}, errors.New("trailing bytes after parsing Merkle proofs; possibly malformed metadata")
	}

	return ZkExecutionISMMetadata{
		ProofType:    proofType,
		Proof:        proof,
		PublicValues: publicInputs,
		MerkleProofs: merkleProofs,
	}, nil
}

// HasExecutionProof returns true if ZkExecutionISMMetadata contains an execution proof, otherwise false.
func (meta ZkExecutionISMMetadata) HasExecutionProof() bool {
	return len(meta.Proof) > 0
}
