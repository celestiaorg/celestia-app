package types_test

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"

	"github.com/celestiaorg/celestia-app/v6/x/zkism/types"
	"github.com/stretchr/testify/require"
)

func encodeUint32(v uint32) []byte {
	bz := make([]byte, 4)
	binary.BigEndian.PutUint32(bz, v)
	return bz
}

func encodeUint64(v uint64) []byte {
	bz := make([]byte, 8)
	binary.BigEndian.PutUint64(bz, v)
	return bz
}

func TestNewZkExecutionISMMetadata(t *testing.T) {
	const height uint64 = 12345

	proof := []byte{0xAA, 0xBB, 0xCC}
	pubInputs := types.StateTransitionPublicValues{
		CelestiaHeaderHash: [32]byte{0x01},
		TrustedHeight:      42,
		TrustedStateRoot:   [32]byte{0x02},
		NewHeight:          50,
		NewStateRoot:       [32]byte{0x03},
		Namespace:          [29]byte{0xCC},
		PublicKey:          [32]byte{0xDD},
	}

	pubInputsBz, err := pubInputs.Marshal()
	require.NoError(t, err)

	merkle1 := bytes.Repeat([]byte{0x03}, 32)
	merkle2 := bytes.Repeat([]byte{0x04}, 32)

	metadata := func() []byte {
		var b []byte
		b = append(b, byte(types.ProofTypeSP1Groth16))           // [0] proof type
		b = append(b, encodeUint64(height)...)                   // [1:9] height (uint64)
		b = append(b, encodeUint32(uint32(len(proof)))...)       // [9:13] proof size
		b = append(b, proof...)                                  // [13:13+N] proof
		b = append(b, encodeUint32(uint32(len(pubInputsBz)))...) // [13+N:17+N] public values size
		b = append(b, pubInputsBz...)                            // [17+N:17+N+M] public values
		b = append(b, merkle1...)                                // merkle proof 1 (32 bytes)
		b = append(b, merkle2...)                                // merkle proof 2 (32 bytes)
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
			name:     "too short for type+height+sizes",
			metadata: []byte{byte(types.ProofTypeSP1Groth16), 0x00}, // < 17 bytes total
			expError: errors.New("metadata too short to contain valid proof data"),
		},
		{
			name:     "invalid proof type",
			metadata: append([]byte{0xFF}, metadata()[1:]...),
			expError: errors.New("invalid proof type: expected 0, got 255"),
		},
		{
			name: "too short for full proof",
			metadata: func() []byte {
				var b []byte
				b = append(b, byte(types.ProofTypeSP1Groth16)) // type
				b = append(b, encodeUint64(height)...)         // height
				b = append(b, encodeUint32(10)...)             // claim N=10
				// no proof bytes present
				return b
			}(),
			expError: errors.New("metadata too short to contain valid proof data"),
		},
		{
			name: "too short for public values size",
			metadata: func() []byte {
				var b []byte
				b = append(b, byte(types.ProofTypeSP1Groth16)) // type
				b = append(b, encodeUint64(height)...)         // height
				b = append(b, encodeUint32(1)...)              // proof size = 1
				b = append(b, 0xAA)                            // 1 byte proof
				b = append(b, 0xFF, 0xFF, 0xFF)                // only 3 bytes of size field
				return b
			}(),
			expError: errors.New("metadata too short to contain public values size"),
		},
		{
			name: "too short for public values",
			metadata: func() []byte {
				var b []byte
				b = append(b, byte(types.ProofTypeSP1Groth16))   // type
				b = append(b, encodeUint64(height)...)           // height
				b = append(b, encodeUint32(1)...)                // proof size = 1
				b = append(b, 0xAA)                              // 1 byte proof
				b = append(b, encodeUint32(100)...)              // public values size = 100
				b = append(b, bytes.Repeat([]byte{0x00}, 50)...) // only 50 present
				return b
			}(),
			expError: errors.New("metadata too short to contain public values: expected 100 bytes"),
		},
		{
			name: "trailing bytes",
			metadata: func() []byte {
				b := metadata()
				b = append(b, 0xFF) // unexpected trailing byte
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
				require.Equal(t, height, result.Height)
				require.Equal(t, proof, result.Proof)
				require.Equal(t, pubInputs, result.PublicValues)
				require.Len(t, result.MerkleProofs, 2)
				require.Equal(t, merkle1, result.MerkleProofs[0])
				require.Equal(t, merkle2, result.MerkleProofs[1])
			}
		})
	}
}

func TestNewZkExecutionISMMetadataNoExecutionProof(t *testing.T) {
	const height uint64 = 777

	testcases := []struct {
		name     string
		metadata []byte
		expError error
	}{
		{
			name: "valid metadata: no zk proof, no public values, only merkle proofs",
			metadata: func() []byte {
				var b []byte
				b = append(b, byte(types.ProofTypeSP1Groth16))   // type
				b = append(b, encodeUint64(height)...)           // height
				b = append(b, encodeUint32(0)...)                // zero-length proof
				b = append(b, encodeUint32(0)...)                // zero public values
				b = append(b, bytes.Repeat([]byte{0xAA}, 32)...) // merkle proof 1
				b = append(b, bytes.Repeat([]byte{0xBB}, 32)...) // merkle proof 2
				return b
			}(),
			expError: nil,
		},
		{
			name: "no zk proof, no public values, only merkle proofs, trailing bytes",
			metadata: func() []byte {
				var b []byte
				b = append(b, byte(types.ProofTypeSP1Groth16))   // type
				b = append(b, encodeUint64(height)...)           // height
				b = append(b, encodeUint32(0)...)                // zero-length proof
				b = append(b, encodeUint32(0)...)                // zero public values
				b = append(b, bytes.Repeat([]byte{0xAA}, 32)...) // merkle proof 1
				b = append(b, bytes.Repeat([]byte{0xBB}, 32)...) // merkle proof 2
				// trailing bytes to trigger error
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
				require.Equal(t, height, result.Height)
				require.Empty(t, result.Proof)
				require.Empty(t, result.PublicValues)
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
