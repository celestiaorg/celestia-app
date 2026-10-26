package groth16_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"math/big"
	"testing"

	"github.com/celestiaorg/celestia-app/v11/x/zkism/internal/groth16"
	curve "github.com/consensys/gnark-crypto/ecc/bn254"
	"github.com/consensys/gnark/backend/witness"
	"github.com/stretchr/testify/require"
)

// The tests in this file pin the github.com/consensys/gnark and gnark-crypto
// behavior that x/zkism depends on. Golden values were recorded with gnark
// v0.16.4 and gnark-crypto v0.22.0. If a gnark upgrade changes any of them, it
// changes which proofs and verifying keys the chain accepts and must not be
// merged without a coordinated network upgrade.

func TestGoldenVerifyingKeyEncoding(t *testing.T) {
	vkBytes, _, _ := readGroth16Testdata(t, "state_transition")
	require.Equal(t, "4388a21c687fdd5f218d7e3d13190cac4c5355818d3605fd5fb811df468ee696", hex.EncodeToString(sha256Sum(vkBytes)))

	vk, err := groth16.NewVerifyingKey(vkBytes)
	require.NoError(t, err)
	require.Equal(t, 5, vk.NbPublicWitness())

	var buf bytes.Buffer
	_, err = vk.WriteTo(&buf)
	require.NoError(t, err)
	require.Equal(t, vkBytes, buf.Bytes())
}

func TestGoldenProofEncoding(t *testing.T) {
	for _, proofDir := range []string{"state_transition", "state_membership"} {
		t.Run(proofDir, func(t *testing.T) {
			_, proofBytes, _ := readGroth16Testdata(t, proofDir)
			gnarkProof := proofBytes[proofPrefixLen+proofMetadataLen:]

			proof, err := groth16.UnmarshalProof(gnarkProof)
			require.NoError(t, err)

			var buf bytes.Buffer
			enc := curve.NewEncoder(&buf, curve.RawEncoding())
			require.NoError(t, enc.Encode(&proof.Ar))
			require.NoError(t, enc.Encode(&proof.Bs))
			require.NoError(t, enc.Encode(&proof.Krs))
			require.Equal(t, gnarkProof, buf.Bytes())
		})
	}
}

func TestGoldenPublicWitnessEncoding(t *testing.T) {
	pubWitness, err := groth16.NewPublicWitness(
		groth16.NewBN254FrElement(big.NewInt(1)),
		groth16.NewBN254FrElement(groth16.HashBN254([]byte("celestia"))),
		groth16.NewBN254FrElement(new(big.Int).Lsh(big.NewInt(1), 253)),
	)
	require.NoError(t, err)

	bz, err := pubWitness.MarshalBinary()
	require.NoError(t, err)
	require.Equal(t, "000000030000000000000003000000000000000000000000000000000000000000000000000000000000000113569e544705849042059c1e3092cd39a934f4501f3078b0218a8e688cbfd0f92000000000000000000000000000000000000000000000000000000000000000", hex.EncodeToString(bz))
}

func TestGoldenVerifyProof(t *testing.T) {
	testCases := []struct {
		proofDir     string
		programVkHex string
	}{
		{"state_transition", stateTransitionProgramVkHex},
		{"state_membership", stateMembershipProgramVkHex},
	}

	for _, tc := range testCases {
		t.Run(tc.proofDir, func(t *testing.T) {
			vkBytes, proofBytes, valuesBytes := readGroth16Testdata(t, tc.proofDir)
			vk, err := groth16.NewVerifyingKey(vkBytes)
			require.NoError(t, err)
			programVk, err := hex.DecodeString(tc.programVkHex)
			require.NoError(t, err)

			pubWitness := sp1PublicWitness(t, proofBytes, programVk, valuesBytes)
			proof, err := groth16.UnmarshalProof(proofBytes[proofPrefixLen+proofMetadataLen:])
			require.NoError(t, err)
			require.NoError(t, groth16.VerifyProof(proof, vk, pubWitness))

			// A different public input must be rejected.
			otherWitness := sp1PublicWitness(t, proofBytes, programVk, append([]byte{0x01}, valuesBytes...))
			require.Error(t, groth16.VerifyProof(proof, vk, otherWitness))

			// Each tampered proof element must be rejected, either when decoding or when verifying.
			gnarkProof := proofBytes[proofPrefixLen+proofMetadataLen:]
			for _, offset := range []int{31, 64 + 127, 64 + 128 + 63} {
				tampered := bytes.Clone(gnarkProof)
				tampered[offset] ^= 0x01
				tamperedProof, err := groth16.UnmarshalProof(tampered)
				if err == nil {
					require.Error(t, groth16.VerifyProof(tamperedProof, vk, pubWitness), "offset %d", offset)
				}
			}
		})
	}
}

// sp1PublicWitness builds the five SP1 v6 public inputs the same way as
// types.SP1Groth16Verifier.VerifyProof.
func sp1PublicWitness(t *testing.T, proofBytes, programVk, valuesBytes []byte) witness.Witness {
	t.Helper()
	exitCode := proofBytes[proofPrefixLen : proofPrefixLen+32]
	vkRoot := proofBytes[proofPrefixLen+32 : proofPrefixLen+64]
	proofNonce := proofBytes[proofPrefixLen+64 : proofPrefixLen+96]

	pubWitness, err := groth16.NewPublicWitness(
		groth16.NewBN254FrElement(new(big.Int).SetBytes(programVk)),
		groth16.NewBN254FrElement(groth16.HashBN254(valuesBytes)),
		groth16.NewBN254FrElement(new(big.Int).SetBytes(exitCode)),
		groth16.NewBN254FrElement(new(big.Int).SetBytes(vkRoot)),
		groth16.NewBN254FrElement(new(big.Int).SetBytes(proofNonce)),
	)
	require.NoError(t, err)
	return pubWitness
}

func sha256Sum(bz []byte) []byte {
	sum := sha256.Sum256(bz)
	return sum[:]
}
