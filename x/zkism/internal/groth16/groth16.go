package groth16

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"math/big"

	"github.com/consensys/gnark-crypto/ecc"
	curve "github.com/consensys/gnark-crypto/ecc/bn254"
	bn254fr "github.com/consensys/gnark-crypto/ecc/bn254/fr"
	"github.com/consensys/gnark/backend"
	"github.com/consensys/gnark/backend/groth16"
	bn254 "github.com/consensys/gnark/backend/groth16/bn254" //nolint:revive,stylecheck
	"github.com/consensys/gnark/backend/witness"
)

// VerifyingKey is a simple type alias for the underlying gnark groth16 VerifyingKey.
type VerifyingKey = groth16.VerifyingKey

// NewVerifyingKey deserializes a Groth16 verifying key for the BN254 curve from a byte slice.
//
// It initializes a new verifying key instance for the BN254 scalar field and reads the
// key data from the provided byte slice. The function ensures that the entire input is
// consumed during deserialization to catch trailing or truncated data.
func NewVerifyingKey(keyBz []byte) (groth16.VerifyingKey, error) {
	vk := groth16.NewVerifyingKey(ecc.BN254)
	n, err := vk.ReadFrom(bytes.NewReader(keyBz))
	if err != nil {
		return nil, fmt.Errorf("error unmarshaling verifier key: %v", err)
	}

	if int(n) != len(keyBz) {
		return nil, fmt.Errorf("invalid key length: expected %d, got %d", len(keyBz), n)
	}

	return vk, nil
}

// VerifyProof verifies a Groth16 proof against a verifying key and a public witness.
// It delegates directly to the gnark/groth16 implementation.
func VerifyProof(proof groth16.Proof, vk groth16.VerifyingKey, publicWitness witness.Witness, opts ...backend.VerifierOption) error {
	return groth16.Verify(proof, vk, publicWitness, opts...)
}

// UnmarshalProof deserializes a Groth16 proof encoded as bytes into a bn254.Proof.
//
// The input byte slice is expected to contain a valid serialized Groth16 proof
// consisting of three elliptic curve elements: Ar, Bs, and Krs. The function
// uses the gnark-crypto curve decoder for BN254 to parse each element in order.
func UnmarshalProof(proofBz []byte) (*bn254.Proof, error) {
	proof := &bn254.Proof{}
	dec := curve.NewDecoder(bytes.NewReader(proofBz))

	if err := dec.Decode(&proof.Ar); err != nil {
		return nil, fmt.Errorf("error unmarshaling proof: %v", err)
	}
	if err := dec.Decode(&proof.Bs); err != nil {
		return nil, fmt.Errorf("error unmarshaling proof: %v", err)
	}
	if err := dec.Decode(&proof.Krs); err != nil {
		return nil, fmt.Errorf("error unmarshaling proof: %v", err)
	}

	return proof, nil
}

// HashBN254 hashes the buffer using SHA-256, masks the top 3 bits, and returns a big.Int
// compliant with BN254 field elements.
func HashBN254(data []byte) *big.Int {
	hash := sha256.Sum256(data)

	// mask the top 3 bits of the first byte (most significant bits)
	hash[0] &= 0b00011111

	return new(big.Int).SetBytes(hash[:])
}

// NewBN254FrElement creates a new BN254 scalar field element from a big.Int.
func NewBN254FrElement(bigInt *big.Int) *bn254fr.Element {
	var elm bn254fr.Element
	return elm.SetBigInt(bigInt)
}

// NewPublicWitness constructs a public witness using the provided input values.
//
// It initializes a new witness over the BN254 scalar field, fills it with the
// given public values, and returns the public portion of the witness.
func NewPublicWitness(inputs ...any) (witness.Witness, error) {
	w, err := witness.New(ecc.BN254.ScalarField())
	if err != nil {
		return nil, fmt.Errorf("error creating witness: %v", err)
	}

	pubInputs := make(chan any, len(inputs))
	for _, v := range inputs {
		pubInputs <- v
	}
	close(pubInputs)

	if err := w.Fill(len(pubInputs), 0, pubInputs); err != nil {
		return nil, fmt.Errorf("error filling witness: %v", err)
	}

	public, err := w.Public()
	if err != nil {
		return nil, fmt.Errorf("error getting public witness: %v", err)
	}

	return public, nil
}
