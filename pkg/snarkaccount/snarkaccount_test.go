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
// - Input Y: The result of squaring X (i.e., X * X).
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

func TestSquareRootPredicate(t *testing.T) {
	args := []struct {
		name    string
		X, Y, Z int64
		valid   bool
	}{
		{"valid witness: 3*3=9", 3, 9, 3, true},
		{"valid witness: 3*3=9", 3, 9, 1, true},
		{"valid witness: 1*1=1", 1, 1, 4, true},
		{"invalid witness: 4*4!=15", 4, 15, 1, false},
	}
	for _, tc := range args {
		t.Run(tc.name, func(t *testing.T) {
			// Compiles the circuit into a R1CS (Rank-1 Constraint System).
			var circuit SquareRoot
			ccs, err := frontend.Compile(ecc.BN254.ScalarField(), r1cs.NewBuilder, &circuit)
			assert.NoError(t, err)

			// Performs the setup phase of the Groth16 zkSNARK.
			pk, vk, err := groth16.Setup(ccs)
			assert.NoError(t, err)

			// Defines the witness (assignment of values to the circuit inputs).
			assignment := SquareRoot{X: tc.X, Y: tc.Y, Z: tc.Z}
			witness, err := frontend.NewWitness(&assignment, ecc.BN254.ScalarField())
			assert.NoError(t, err)

			publicWitness, err := witness.Public()
			assert.NoError(t, err)

			// Generates a proof using the Groth16 proving system.
			proof, err := groth16.Prove(ccs, pk, witness)
			if tc.valid {
				assert.NoError(t, err)
			} else { // no proof can be generated for invalid witnesses
				assert.Error(t, err)
				return // no need to continue, as no proof was generated
			}

			// Verifies the proof against the verification key and the public witness.
			err = groth16.Verify(proof, vk, publicWitness)
			if tc.valid {
				assert.NoError(t, err) // lack of error indicates that the proof is valid
			} else {
				assert.Error(t, err)
			}
		})
	}
}
