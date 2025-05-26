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

// ZkExecutionISMMetadata contains the ZK proof and verification data.
type ZkExecutionISMMetadata struct {
	ProofType    ProofType // Type of ZK Proof System used. Default: Groth16
	Proof        []byte    // The ZK proof bytes
	PublicInputs [][]byte  // Public inputs to the proof verification
	MerkleProofs [][]byte  // The Merkle Proof bytes
}

// NewZkExecutionISMMetadata parses a raw metadata byte slice into a structured format.
// The ZK Execution ISM metadata follows the following format:
// [0]			- Type of the ZK Proof System used (e.g. Groth16)
// [1:5] 		- Size of the ZK Proof, N, if it exists
// [5:N+5] 		- The ZK proof
// [N+5:N+9] 	- Number of public inputs, k
// [N+9:N+9+k]	- Public inputs
// [N+9+k:] 	- Merkle Proofs
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

	// [1:5] - Size of the proof, N (uint32)
	proofSize := binary.BigEndian.Uint32(metadata[offset : offset+4])
	offset += 4
	if len(metadata[offset:]) < int(proofSize) {
		return ZkExecutionISMMetadata{}, fmt.Errorf("metadata too short to contain full proof: expected %d bytes", proofSize)
	}

	// [5:N+5] - ZK proof
	proof := metadata[offset : offset+int(proofSize)]
	offset += int(proofSize)

	// [N+5:N+9] - Number of public inputs, k (uint32)
	if len(metadata[offset:]) < 4 {
		return ZkExecutionISMMetadata{}, errors.New("metadata too short to contain number of public inputs")
	}

	numPublicInputs := binary.BigEndian.Uint32(metadata[offset : offset+4])
	offset += 4

	// [N+9:N+9+k*32] - Public inputs (each assumed to be 32 bytes)
	publicInputs := make([][]byte, 0, numPublicInputs)
	for i := uint32(0); i < numPublicInputs; i++ {
		if len(metadata[offset:]) < 32 {
			return ZkExecutionISMMetadata{}, fmt.Errorf("metadata too short for public input %d", i)
		}

		publicInputs = append(publicInputs, metadata[offset:offset+32])
		offset += 32
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
		PublicInputs: publicInputs,
		MerkleProofs: merkleProofs,
	}, nil
}
