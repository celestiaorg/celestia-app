package keeper

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v10/fibre/validator"
	"github.com/cometbft/cometbft/crypto/ed25519"
	cmtmath "github.com/cometbft/cometbft/libs/math"
	core "github.com/cometbft/cometbft/types"
	"github.com/stretchr/testify/require"
)

func TestExpandedPositionalSignaturesMatchesSignatureSet(t *testing.T) {
	signBytes := []byte("positional certificate")
	powers := []int64{10, 15, 20, 25, 30}
	validators := make([]*core.Validator, len(powers))
	valid := make([][]byte, len(powers))
	invalid := make([][]byte, len(powers))
	for i, power := range powers {
		key := ed25519.GenPrivKey()
		validators[i] = core.NewValidator(key.PubKey(), power)
		valid[i], _ = key.Sign(signBytes)
		invalid[i], _ = key.Sign([]byte("wrong message"))
	}
	converted := &convertedValset{validators: validators, set: core.NewValidatorSet(validators)}
	valSet := validator.Set{ValidatorSet: converted.set}
	for mask := range 3 * 3 * 3 * 3 * 3 {
		choices := mask
		signatures := make([][]byte, len(validators))
		for i := range signatures {
			switch choices % 3 {
			case 1:
				signatures[i] = valid[i]
			case 2:
				signatures[i] = invalid[i]
			}
			choices /= 3
		}
		sigSet := valSet.NewSignatureSet(cmtmath.Fraction{Numerator: 2, Denominator: 3}, signBytes)
		var expected error
		for i, signature := range signatures {
			if len(signature) == 0 {
				continue
			}
			var enough bool
			enough, expected = sigSet.Add(validators[i], signature)
			if expected != nil || enough {
				break
			}
		}
		if expected == nil {
			_, expected = sigSet.Signatures()
		}
		actual := validateExpandedPositionalSignatures(converted, signBytes, signatures)
		require.Equalf(t, expected == nil, actual == nil, "mask %d: expected %v, got %v", mask, expected, actual)
		parallel := validateCheckTxPositionalSignatures(converted, signBytes, signatures)
		require.Equalf(t, expected == nil, parallel == nil, "mask %d: expected %v, got %v", mask, expected, parallel)
	}
}
