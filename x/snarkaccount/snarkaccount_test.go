package snarkaccount_test

import (
	"testing"

	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/frontend/cs/r1cs"
	"github.com/stretchr/testify/assert"
)

// SquareRoot represents a simple circuit to compute the square root.
// It defines three inputs::
// - Input X: The base of the square operation.
// - Input Z: An additional input with no effect on the constraints.
// - Output Y: The result of squaring X (i.e., X * X).
type SquareRoot struct {
	X frontend.Variable `gnark:"x"`       // Base of the square operation
	Y frontend.Variable `gnark:",public"` // Result of the square operation
	Z frontend.Variable `gnark:",public"` // Additional input, but not used in constraints
}

// Define sets up the circuit's constraints.
// It ensures that the square of X equals Y (X*X == Y).
// Z is intentionally left unused.
func (circuit *SquareRoot) Define(api frontend.API) error {
	x2 := api.Mul(circuit.X, circuit.X) // Multiplies X with itself
	api.AssertIsEqual(circuit.Y, x2)    // Asserts that Y is equal to the square of X
	return nil
}

func Test(t *testing.T) {
	// Compiles the circuit into a R1CS (Rank-1 Constraint System).
	var circuit SquareRoot
	ccs, _ := frontend.Compile(ecc.BN254.ScalarField(), r1cs.NewBuilder, &circuit)

	// Performs the setup phase of the Groth16 zkSNARK.
	pk, vk, _ := groth16.Setup(ccs)

	// Defines the witness (assignment of values to the circuit inputs).
	assignment := SquareRoot{X: 3, Y: 9, Z: 3}
	witness, _ := frontend.NewWitness(&assignment, ecc.BN254.ScalarField())
	publicWitness, _ := witness.Public()

	// Generates a proof using the Groth16 proving system.
	proof, _ := groth16.Prove(ccs, pk, witness)

	// Verifies the proof against the verification key and the public witness.
	err := groth16.Verify(proof, vk, publicWitness)
	assert.NoError(t, err) // lack of error indicates that the proof is valid
}
