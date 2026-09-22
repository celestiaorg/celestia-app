package validator_test

import (
	"math/rand/v2"
	"testing"

	"github.com/celestiaorg/celestia-app/v10/fibre/validator"
	"github.com/cometbft/cometbft/crypto/ed25519"
	cmtmath "github.com/cometbft/cometbft/libs/math"
	core "github.com/cometbft/cometbft/types"
	"github.com/stretchr/testify/require"
)

// TestQuorumPrefixMatchesSequentialWalk pins the contract a parallel verifier
// depends on: QuorumPrefix returns exactly the entries a sequential
// SignatureSet.Add walk inspects before it short-circuits. Entries past the
// prefix are never verified today, so a parallel verifier must not verify them
// either, or it would reject messages the chain accepts.
func TestQuorumPrefixMatchesSequentialWalk(t *testing.T) {
	twoThirds := cmtmath.Fraction{Numerator: 2, Denominator: 3}
	rng := rand.New(rand.NewPCG(1, 2))
	signBytes := []byte("quorum prefix test")

	for _, numVals := range []int{1, 4, 10, 37, 100} {
		for range 20 {
			validators := make([]*core.Validator, numVals)
			privKeys := make([]ed25519.PrivKey, numVals)
			for i := range numVals {
				privKeys[i] = ed25519.GenPrivKey()
				validators[i] = core.NewValidator(privKeys[i].PubKey(), int64(1+rng.IntN(50)))
			}
			valSet := validator.Set{ValidatorSet: core.NewValidatorSet(validators), Height: 1}

			// Sign with a random subset; unsigned slots stay empty.
			signatures := make([][]byte, numVals)
			for i := range numVals {
				if rng.IntN(4) == 0 {
					continue
				}
				sig, err := privKeys[i].Sign(signBytes)
				require.NoError(t, err)
				signatures[i] = sig
			}

			// Walk the set the way the keeper does and record where it stops.
			sigSet := valSet.NewSignatureSet(twoThirds, signBytes)
			inspected := len(signatures)
			for i, signature := range signatures {
				if len(signature) == 0 {
					continue
				}
				hasEnough, err := sigSet.Add(validators[i], signature)
				require.NoError(t, err)
				if hasEnough {
					inspected = i + 1
					break
				}
			}
			_, quorumErr := sigSet.Signatures()

			prefix, quorumMet := validator.QuorumPrefix(validators, signatures, valSet.MinRequiredVotingPower(twoThirds))
			require.Equal(t, inspected, prefix)
			require.Equal(t, quorumErr == nil, quorumMet)
		}
	}
}
