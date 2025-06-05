package types_test

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/celestiaorg/celestia-app/v4/x/zkism/types"
)

func encodeUint32(v uint32) []byte {
	bz := make([]byte, 4)
	binary.BigEndian.PutUint32(bz, v)
	return bz
}

func TestNewZkExecutionISMMetadata(t *testing.T) {
	proof := []byte{0xAA, 0xBB, 0xCC}
	pubInputs := types.PublicInputs{
		TrustedStateRoot:     [32]byte{},
		NewHeaderHash:        [32]byte{},
		PreviousHeaderHash:   [32]byte{},
		CelestiaHeaderHashes: [][]byte{bytes.Repeat([]byte{0x01}, 32), bytes.Repeat([]byte{0x02}, 32)},
		NewStateRoot:         [32]byte{},
		NewHeight:            42,
	}

	pubInputsBz, err := pubInputs.Marshal()
	require.NoError(t, err)

	merkle1 := bytes.Repeat([]byte{0x03}, 32)
	merkle2 := bytes.Repeat([]byte{0x04}, 32)

	metadata := func() []byte {
		var b []byte
		b = append(b, byte(types.ProofTypeSP1Groth16))           // proof type
		b = append(b, encodeUint32(uint32(len(proof)))...)       // proof size
		b = append(b, proof...)                                  // proof
		b = append(b, encodeUint32(uint32(len(pubInputsBz)))...) // public inputs size
		b = append(b, pubInputsBz...)                            // public inputs bytes
		b = append(b, merkle1...)                                // merkle proof 1
		b = append(b, merkle2...)                                // merkle proof 2
		return b
	}

	testcases := []struct {
		name     string
		metadata []byte
		expError error
	}{
		{
			name:     "valid metadata",
			metadata: metadata(),
			expError: nil,
		},
		{
			name:     "too short for proof size",
			metadata: []byte{byte(types.ProofTypeSP1Groth16), 0x00, 0x00},
			expError: errors.New("metadata too short to contain proof type and size"),
		},
		{
			name:     "invalid proof type",
			metadata: append([]byte{0xFF}, metadata()[1:]...),
			expError: errors.New("invalid proof type: expected 0, got 255"),
		},
		{
			name:     "too short for full proof",
			metadata: append([]byte{byte(types.ProofTypeSP1Groth16)}, encodeUint32(10)...), // define 10-byte proof but provide none
			expError: errors.New("metadata too short to contain full proof: expected 10 bytes"),
		},
		{
			name: "too short for public inputs length",
			metadata: func() []byte {
				b := append([]byte{byte(types.ProofTypeSP1Groth16)}, encodeUint32(1)...)
				b = append(b, 0xAA)
				b = append(b, 0xFF, 0xFF, 0xFF) // 3 bytes is insufficient for uint32
				return b
			}(),
			expError: errors.New("metadata too short to contain number of public inputs"),
		},
		{
			name: "too short for public inputs",
			metadata: func() []byte {
				b := append([]byte{byte(types.ProofTypeSP1Groth16)}, encodeUint32(1)...)
				b = append(b, 0xAA)
				b = append(b, encodeUint32(100)...)              // 100 bytes for PublicInputs
				b = append(b, bytes.Repeat([]byte{0x00}, 50)...) // only provide 50
				return b
			}(),
			expError: errors.New("metadata too short to contain public inputs: expected 100 bytes"),
		},
		{
			name: "trailing bytes",
			metadata: func() []byte {
				b := metadata()
				b = append(b, 0xFF) // extra byte
				return b
			}(),
			expError: errors.New("trailing bytes after parsing Merkle proofs; possibly malformed metadata"),
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := types.NewZkExecutionISMMetadata(tc.metadata)

			if tc.expError != nil {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.expError.Error())
			} else {
				require.NoError(t, err)
				require.Equal(t, types.ProofTypeSP1Groth16, result.ProofType)
				require.Equal(t, proof, result.Proof)
				require.Equal(t, pubInputs, result.PublicInputs)
				require.Len(t, result.MerkleProofs, 2)
				require.Equal(t, merkle1, result.MerkleProofs[0])
				require.Equal(t, merkle2, result.MerkleProofs[1])
			}
		})
	}
}

func TestNewZkExecutionISMMetadataNoExecutionProof(t *testing.T) {
	testcases := []struct {
		name     string
		metadata []byte
		expError error
	}{
		{
			name: "valid metadata: no zk proof, no public inputs, only merkle proofs",
			metadata: func() []byte {
				b := []byte{byte(types.ProofTypeSP1Groth16)}     // proof type
				b = append(b, encodeUint32(0)...)                // zero-length proof
				b = append(b, encodeUint32(0)...)                // zero public inputs
				b = append(b, bytes.Repeat([]byte{0xAA}, 32)...) // merkle proof 1
				b = append(b, bytes.Repeat([]byte{0xBB}, 32)...) // merkle proof 2
				return b
			}(),
			expError: nil,
		},
		{
			name: "no zk proof, no public inputs, only merkle proofs, trailing bytes",
			metadata: func() []byte {
				b := []byte{byte(types.ProofTypeSP1Groth16)}     // proof type
				b = append(b, encodeUint32(0)...)                // zero-length proof
				b = append(b, encodeUint32(0)...)                // zero public inputs
				b = append(b, bytes.Repeat([]byte{0xAA}, 32)...) // merkle proof 1
				b = append(b, bytes.Repeat([]byte{0xBB}, 32)...) // merkle proof 2

				// append trailing bytes to trigger error
				b = append(b, bytes.Repeat([]byte{0xFF}, 10)...)
				return b
			}(),
			expError: errors.New("trailing bytes after parsing Merkle proofs; possibly malformed metadata"),
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := types.NewZkExecutionISMMetadata(tc.metadata)

			if tc.expError != nil {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.expError.Error())
			} else {
				require.NoError(t, err)
				require.Equal(t, types.ProofTypeSP1Groth16, result.ProofType)
				require.Empty(t, result.Proof)
				require.Empty(t, result.PublicInputs)

				require.Len(t, result.MerkleProofs, 2)
				require.Equal(t, bytes.Repeat([]byte{0xAA}, 32), result.MerkleProofs[0])
				require.Equal(t, bytes.Repeat([]byte{0xBB}, 32), result.MerkleProofs[1])
			}
		})
	}
}

func TestHasExecutionProof(t *testing.T) {
	metadata := types.ZkExecutionISMMetadata{
		ProofType: types.ProofTypeSP1Groth16,
		Proof:     []byte{0xAA, 0xBB, 0xCC},
	}

	has := metadata.HasExecutionProof()
	require.True(t, has)

	metadata.Proof = nil

	has = metadata.HasExecutionProof()
	require.False(t, has)
}
