package groth16_test

import (
	"encoding/hex"
	"math/big"
	"os"
	"testing"

	"github.com/celestiaorg/celestia-app/v7/x/zkism/internal/groth16"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
)

const (
	stateTransitionProgramVkHex  = "0017bc91d53b93c46eb842d7f9020a94ea13d8877a21608b34b71fcc4da64f29"
	stateMembershipProgramVkHex  = "004959d5fb2c3d5bc1f98e032188dd94fbb5c6b6152df356c7c20be23be824a2"
	proofPrefixLen               = 4
)

func BenchmarkGroth16VerifyProof(b *testing.B) {
	vkBytes, proofBytes, valuesBytes := readGroth16Testdata(b, "state_transition")

	vk, err := groth16.NewVerifyingKey(vkBytes)
	if err != nil {
		b.Fatal(err)
	}

	if len(proofBytes) < proofPrefixLen {
		b.Fatalf("proof bytes too short: %d", len(proofBytes))
	}

	proof, err := groth16.UnmarshalProof(proofBytes[proofPrefixLen:])
	if err != nil {
		b.Fatal(err)
	}

	programVk, err := hex.DecodeString(stateTransitionProgramVkHex)
	if err != nil {
		b.Fatal(err)
	}

	vkCommitment := new(big.Int).SetBytes(programVk)
	vkElement := groth16.NewBN254FrElement(vkCommitment)
	inputsElement := groth16.NewBN254FrElement(groth16.HashBN254(valuesBytes))

	pubWitness, err := groth16.NewPublicWitness(vkElement, inputsElement)
	if err != nil {
		b.Fatal(err)
	}

	if err := groth16.VerifyProof(proof, vk, pubWitness); err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
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

	if len(proofBytes) < proofPrefixLen {
		b.Fatalf("proof bytes too short: %d", len(proofBytes))
	}

	proof, err := groth16.UnmarshalProof(proofBytes[proofPrefixLen:])
	if err != nil {
		b.Fatal(err)
	}

	programVk, err := hex.DecodeString(stateMembershipProgramVkHex)
	if err != nil {
		b.Fatal(err)
	}

	vkCommitment := new(big.Int).SetBytes(programVk)
	vkElement := groth16.NewBN254FrElement(vkCommitment)
	inputsElement := groth16.NewBN254FrElement(groth16.HashBN254(valuesBytes))

	pubWitness, err := groth16.NewPublicWitness(vkElement, inputsElement)
	if err != nil {
		b.Fatal(err)
	}

	if err := groth16.VerifyProof(proof, vk, pubWitness); err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
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
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
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
