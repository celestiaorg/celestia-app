package groth16_test

import (
	"encoding/hex"
	"math/big"
	"os"
	"testing"

	"github.com/celestiaorg/celestia-app/v9/x/zkism/internal/groth16"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
)

const (
	stateTransitionProgramVkHex = "004ac29c473e811dece0f8dd76c8eda80f886d263efb393ec81f54173e54f160"
	stateMembershipProgramVkHex = "00982fb21526d096c8bf58eda36b5e293ee9ea0f36df441f6a996a974f8feb63"
	proofPrefixLen              = 4
	// proofMetadataLen is the SP1 v6 metadata between the vkey-hash prefix and
	// the gnark proof: exit_code(32) + vk_root(32) + proof_nonce(32).
	proofMetadataLen = 96
)

func BenchmarkGroth16VerifyProof(b *testing.B) {
	vkBytes, proofBytes, valuesBytes := readGroth16Testdata(b, "state_transition")

	vk, err := groth16.NewVerifyingKey(vkBytes)
	if err != nil {
		b.Fatal(err)
	}

	if len(proofBytes) < proofPrefixLen+proofMetadataLen {
		b.Fatalf("proof bytes too short: %d", len(proofBytes))
	}

	// SP1 v6 layout: prefix(4) | exit_code(32) | vk_root(32) | proof_nonce(32) | gnark proof.
	exitCode := proofBytes[proofPrefixLen : proofPrefixLen+32]
	vkRoot := proofBytes[proofPrefixLen+32 : proofPrefixLen+64]
	proofNonce := proofBytes[proofPrefixLen+64 : proofPrefixLen+96]

	proof, err := groth16.UnmarshalProof(proofBytes[proofPrefixLen+proofMetadataLen:])
	if err != nil {
		b.Fatal(err)
	}

	programVk, err := hex.DecodeString(stateTransitionProgramVkHex)
	if err != nil {
		b.Fatal(err)
	}

	vkElement := groth16.NewBN254FrElement(new(big.Int).SetBytes(programVk))
	inputsElement := groth16.NewBN254FrElement(groth16.HashBN254(valuesBytes))
	exitCodeElement := groth16.NewBN254FrElement(new(big.Int).SetBytes(exitCode))
	vkRootElement := groth16.NewBN254FrElement(new(big.Int).SetBytes(vkRoot))
	proofNonceElement := groth16.NewBN254FrElement(new(big.Int).SetBytes(proofNonce))

	pubWitness, err := groth16.NewPublicWitness(vkElement, inputsElement, exitCodeElement, vkRootElement, proofNonceElement)
	if err != nil {
		b.Fatal(err)
	}

	if err := groth16.VerifyProof(proof, vk, pubWitness); err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()

	for b.Loop() {
		if err := groth16.VerifyProof(proof, vk, pubWitness); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkGroth16VerifyProofStateMembership(b *testing.B) {
	vkBytes, proofBytes, valuesBytes := readGroth16Testdata(b, "state_membership")

	vk, err := groth16.NewVerifyingKey(vkBytes)
	if err != nil {
		b.Fatal(err)
	}

	if len(proofBytes) < proofPrefixLen+proofMetadataLen {
		b.Fatalf("proof bytes too short: %d", len(proofBytes))
	}

	// SP1 v6 layout: prefix(4) | exit_code(32) | vk_root(32) | proof_nonce(32) | gnark proof.
	exitCode := proofBytes[proofPrefixLen : proofPrefixLen+32]
	vkRoot := proofBytes[proofPrefixLen+32 : proofPrefixLen+64]
	proofNonce := proofBytes[proofPrefixLen+64 : proofPrefixLen+96]

	proof, err := groth16.UnmarshalProof(proofBytes[proofPrefixLen+proofMetadataLen:])
	if err != nil {
		b.Fatal(err)
	}

	programVk, err := hex.DecodeString(stateMembershipProgramVkHex)
	if err != nil {
		b.Fatal(err)
	}

	vkElement := groth16.NewBN254FrElement(new(big.Int).SetBytes(programVk))
	inputsElement := groth16.NewBN254FrElement(groth16.HashBN254(valuesBytes))
	exitCodeElement := groth16.NewBN254FrElement(new(big.Int).SetBytes(exitCode))
	vkRootElement := groth16.NewBN254FrElement(new(big.Int).SetBytes(vkRoot))
	proofNonceElement := groth16.NewBN254FrElement(new(big.Int).SetBytes(proofNonce))

	pubWitness, err := groth16.NewPublicWitness(vkElement, inputsElement, exitCodeElement, vkRootElement, proofNonceElement)
	if err != nil {
		b.Fatal(err)
	}

	if err := groth16.VerifyProof(proof, vk, pubWitness); err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()

	for b.Loop() {
		if err := groth16.VerifyProof(proof, vk, pubWitness); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSecp256k1VerifySignature(b *testing.B) {
	privKey := secp256k1.GenPrivKey()
	msg := []byte("celestia-secp256k1-benchmark")
	sig, err := privKey.Sign(msg)
	if err != nil {
		b.Fatal(err)
	}

	pubKey := privKey.PubKey()
	if !pubKey.VerifySignature(msg, sig) {
		b.Fatal("signature verification failed")
	}

	b.ReportAllocs()

	for b.Loop() {
		if !pubKey.VerifySignature(msg, sig) {
			b.Fatal("signature verification failed")
		}
	}
}

func readGroth16Testdata(tb testing.TB, proofDir string) (vkBytes, proofBytes, valuesBytes []byte) {
	tb.Helper()

	var err error
	vkBytes, err = os.ReadFile("../testdata/groth16_vk.bin")
	if err != nil {
		tb.Fatalf("failed to read verifier key file: %v", err)
	}

	proofBytes, err = os.ReadFile("../testdata/" + proofDir + "/proof.bin")
	if err != nil {
		tb.Fatalf("failed to read proof file: %v", err)
	}

	valuesBytes, err = os.ReadFile("../testdata/" + proofDir + "/public_values.bin")
	if err != nil {
		tb.Fatalf("failed to read public values file: %v", err)
	}

	return vkBytes, proofBytes, valuesBytes
}
