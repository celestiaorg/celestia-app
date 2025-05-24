package types

import (
	"encoding/binary"
	"errors"
	"fmt"
)

type ProofType uint8

const (
	ProofTypeGroth16 ProofType = iota
)

// ZkExecutionISMMetadata contains the ZK proof and verification data
type ZkExecutionISMMetadata struct {
	ProofType    ProofType // Type of ZK Proof System used. Default: Groth16
	Proof        []byte    // The ZK proof bytes
	PublicInputs [][]byte  // Public inputs to the proof verification
	MerkleProofs [][]byte  // The Merkle Proof bytes
}

// NewZkExecutionISMMetadata parses raw metadata into a structured format
func NewZkExecutionISMMetadata(metadata []byte) (ZkExecutionISMMetadata, error) {
	if len(metadata) < 5 {
		return ZkExecutionISMMetadata{}, errors.New("metadata too short to contain proof type and size")
	}

	offset := 0

	// [0] - Type of the ZK Proof System used
	proofType := ProofType(metadata[offset])
	if proofType != ProofTypeGroth16 {
		return ZkExecutionISMMetadata{}, fmt.Errorf("invalid proof type: expected %d, got %d", ProofTypeGroth16, proofType)
	}

	offset++
	if len(metadata[offset:]) < 4 {
		return ZkExecutionISMMetadata{}, errors.New("metadata too short to contain proof size")
	}

	// [1:5] - Size of the ZK Proof, N (uint32)
	proofSize := binary.BigEndian.Uint32(metadata[offset : offset+4])
	offset += 4

	// [5:N+5] - ZK proof
	if len(metadata[offset:]) < int(proofSize) {
		return ZkExecutionISMMetadata{}, fmt.Errorf("metadata too short to contain full proof: expected %d bytes", proofSize)
	}
	proof := metadata[offset : offset+int(proofSize)]
	offset += int(proofSize)

	// [N+5:N+9] - Number of public inputs, k (uint32)
	if len(metadata[offset:]) < 4 {
		return ZkExecutionISMMetadata{}, errors.New("metadata too short to contain number of public inputs")
	}
	publicInputsCount := binary.BigEndian.Uint32(metadata[offset : offset+4])
	offset += 4

	// [N+9:N+9+k*32] - Public inputs (each assumed to be 32 bytes)
	publicInputs := make([][]byte, 0, publicInputsCount)
	for i := uint32(0); i < publicInputsCount; i++ {
		if len(metadata[offset:]) < 32 {
			return ZkExecutionISMMetadata{}, fmt.Errorf("metadata too short for public input %d", i)
		}
		publicInputs = append(publicInputs, metadata[offset:offset+32])
		offset += 32
	}

	// [remaining] - Merkle Proofs (assume each is 32 bytes as well)
	merkleProofs := make([][]byte, 0)
	for len(metadata[offset:]) >= 32 {
		merkleProofs = append(merkleProofs, metadata[offset:offset+32])
		offset += 32
	}

	// If there's leftover data less than 32 bytes, it's likely malformed
	if len(metadata[offset:]) > 0 {
		return ZkExecutionISMMetadata{}, errors.New("trailing bytes after parsing Merkle proofs; possibly malformed metadata")
	}

	return ZkExecutionISMMetadata{
		ProofType:    proofType,
		Proof:        proof,
		PublicInputs: publicInputs,
		MerkleProofs: merkleProofs,
	}, nil
}
