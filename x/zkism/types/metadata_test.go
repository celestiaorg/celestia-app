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
	buf := make([]byte, 4)
	binary.BigEndian.PutUint32(buf, v)
	return buf
}

func TestNewZkExecutionISMMetadata(t *testing.T) {
	proof := []byte{0xAA, 0xBB, 0xCC}
	publicInput1 := bytes.Repeat([]byte{0x01}, 32)
	publicInput2 := bytes.Repeat([]byte{0x02}, 32)
	merkle1 := bytes.Repeat([]byte{0x03}, 32)
	merkle2 := bytes.Repeat([]byte{0x04}, 32)

	metadata := func() []byte {
		var b []byte
		b = append(b, byte(types.ProofTypeGroth16))        // proof type
		b = append(b, encodeUint32(uint32(len(proof)))...) // proof size
		b = append(b, proof...)                            // proof
		b = append(b, encodeUint32(2)...)                  // number of public inputs
		b = append(b, publicInput1...)                     // public input 1
		b = append(b, publicInput2...)                     // public input 2
		b = append(b, merkle1...)                          // merkle proof 1
		b = append(b, merkle2...)                          // merkle proof 2
		return b
	}

	tests := []struct {
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
			metadata: []byte{byte(types.ProofTypeGroth16), 0x00, 0x00},
			expError: errors.New("metadata too short to contain proof type and size"),
		},
		{
			name:     "invalid proof type",
			metadata: append([]byte{0xFF}, metadata()[1:]...),
			expError: errors.New("invalid proof type: expected 0, got 255"),
		},
		{
			name:     "too short for full proof",
			metadata: append([]byte{byte(types.ProofTypeGroth16)}, encodeUint32(10)...), // define 10-byte proof but provide none
			expError: errors.New("metadata too short to contain full proof: expected 10 bytes"),
		},
		{
			name: "too short for number of public inputs",
			metadata: func() []byte {
				b := append([]byte{byte(types.ProofTypeGroth16)}, encodeUint32(1)...)
				b = append(b, 0xAA)
				b = append(b, 0xFF, 0xFF, 0xFF) // 3 bytes is insufficient for uint32
				return b
			}(),
			expError: errors.New("metadata too short to contain number of public inputs"),
		},
		{
			name: "too short for public input",
			metadata: func() []byte {
				b := append([]byte{byte(types.ProofTypeGroth16)}, encodeUint32(1)...)
				b = append(b, 0xAA)
				b = append(b, encodeUint32(1)...)
				b = append(b, bytes.Repeat([]byte{0x00}, 16)...) // only half of required 32 bytes
				return b
			}(),
			expError: errors.New("metadata too short for public input 0"),
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

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := types.NewZkExecutionISMMetadata(tt.metadata)

			if tt.expError != nil {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.expError.Error())
			} else {
				require.NoError(t, err)
				require.Equal(t, types.ProofTypeGroth16, result.ProofType)
				require.Equal(t, proof, result.Proof)
				require.Len(t, result.PublicInputs, 2)
				require.Equal(t, publicInput1, result.PublicInputs[0])
				require.Equal(t, publicInput2, result.PublicInputs[1])
				require.Len(t, result.MerkleProofs, 2)
				require.Equal(t, merkle1, result.MerkleProofs[0])
				require.Equal(t, merkle2, result.MerkleProofs[1])
			}
		})
	}
}
