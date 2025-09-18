package groth16_test

import (
	"crypto/sha256"
	"math/big"
	"os"
	"testing"

	"github.com/celestiaorg/celestia-app/v6/x/zkism/internal/groth16"
	"github.com/consensys/gnark-crypto/ecc"
	bn254fr "github.com/consensys/gnark-crypto/ecc/bn254/fr"
	"github.com/stretchr/testify/require"
)

func TestNewVerifyingKey(t *testing.T) {
	verifierKeyBz, err := os.ReadFile("../testdata/groth16_vk.bin")
	require.NoError(t, err, "failed to read verifier key file")

	vk, err := groth16.NewVerifyingKey(verifierKeyBz)
	require.NoError(t, err)
	require.Equal(t, ecc.BN254, vk.CurveID())
}

func TestUnmarshalProof(t *testing.T) {
	proofBz, err := os.ReadFile("../testdata/state_transition/proof.bin")
	require.NoError(t, err, "failed to read proof file")

	// discard the first 4 bytes as with SP1 this is a prefix of the first 4 bytes of the verifier key hash
	proofBz = proofBz[4:]

	proof, err := groth16.UnmarshalProof(proofBz)
	require.NoError(t, err, "failed to unmarshal proof")
	require.NotNil(t, proof)

	// sanity checks that the proof components are non-zero
	require.False(t, proof.Ar.IsInfinity(), "Ar should not be point at infinity")
	require.False(t, proof.Bs.IsInfinity(), "Bs should not be point at infinity")
	require.False(t, proof.Krs.IsInfinity(), "Krs should not be point at infinity")
}

func TestHashBN254(t *testing.T) {
	input := []byte("just trust me bro")

	result := groth16.HashBN254(input)
	require.NotNil(t, result)
	require.True(t, result.Sign() >= 0)

	expected := sha256.Sum256(input)
	expected[0] &= 0b00011111
	expectedInt := new(big.Int).SetBytes(expected[:])

	require.Equal(t, 0, result.Cmp(expectedInt), "hash mismatch after masking")

	// assert the top 3 bits are actually zero
	hashBytes := result.Bytes()
	topByte := hashBytes[0]
	require.Equal(t, topByte&0b11100000, byte(0), "top 3 bits are not zero")
}

func TestNewPublicWitness(t *testing.T) {
	inputs := []any{
		groth16.NewBN254FrElement(big.NewInt(256)),
		groth16.NewBN254FrElement(big.NewInt(1000000)),
	}

	pubWitness, err := groth16.NewPublicWitness(inputs...)
	require.NoError(t, err)
	require.NotNil(t, pubWitness)

	require.Len(t, pubWitness.Vector(), len(inputs))

	vec, ok := pubWitness.Vector().(bn254fr.Vector)
	require.True(t, ok)
	require.Equal(t, inputs[0], &vec[0])
	require.Equal(t, inputs[1], &vec[1])
}
