package keeper

import (
	stded25519 "crypto/ed25519"
	"testing"

	naryaed25519 "github.com/Overclock-Validator/narya-ed25519/ed25519"
	"github.com/celestiaorg/celestia-app/v10/fibre/validator"
	"github.com/cometbft/cometbft/crypto/ed25519"
	cmtmath "github.com/cometbft/cometbft/libs/math"
	core "github.com/cometbft/cometbft/types"
	voied25519 "github.com/oasisprotocol/curve25519-voi/primitives/ed25519"
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
	converted.pubs = make([][32]byte, len(validators))
	for i, val := range validators {
		copy(converted.pubs[i][:], val.PubKey.Bytes())
	}
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
		original := validateExpandedPositionalSignaturesVOI(converted, signBytes, signatures)
		require.Equalf(t, errorText(original), errorText(actual), "mask %d", mask)
		parallel := validateCheckTxPositionalSignatures(converted, signBytes, signatures)
		require.Equalf(t, expected == nil, parallel == nil, "mask %d: expected %v, got %v", mask, expected, parallel)
	}
}

// The accelerated path must preserve the exact cofactorless predicate even
// for malformed encodings. Verify a batch of independent verdicts against both
// the existing VOI implementation and Go's standard library.
func TestNaryaStdlibCompatMatchesVOI(t *testing.T) {
	naryaed25519.SetDefaultProfile(naryaed25519.StdlibCompat)
	_ = naryaed25519.SetBackend("r51") // Generic fallback on machines without IFMA.
	const count = 32*7 + 1
	keys := make([][32]byte, count)
	pubs := make([]*[32]byte, count)
	msgs := make([][]byte, count)
	sigs := make([][]byte, count)
	for i := range count - 1 {
		var seed [32]byte
		seed[0] = byte(i/7 + 1)
		priv := stded25519.NewKeyFromSeed(seed[:])
		copy(keys[i][:], priv.Public().(stded25519.PublicKey))
		pubs[i] = &keys[i]
		msgs[i] = []byte{byte(i), byte(i >> 8), 1}
		sigs[i] = stded25519.Sign(priv, msgs[i])
		switch i % 7 {
		case 1:
			sigs[i][0] ^= 1
		case 2:
			msgs[i][0] ^= 1
		case 3:
			sigs[i] = sigs[i][:63]
		case 4:
			for j := range 32 {
				sigs[i][j] = 0xff
			}
		case 5:
			for j := 32; j < 64; j++ {
				sigs[i][j] = 0xff
			}
		case 6:
			sigs[i] = append(sigs[i], 0)
		}
	}
	// A small-order key and R exercise the permissive part of StdlibCompat.
	keys[count-1][0] = 1
	pubs[count-1] = &keys[count-1]
	msgs[count-1] = []byte("small-order public key")
	sigs[count-1] = make([]byte, 64)
	sigs[count-1][0] = 1

	got := make([]bool, count)
	naryaed25519.VerifyBatch(pubs, msgs, sigs, got)
	for i := range got {
		wantStdlib := stded25519.Verify(pubs[i][:], msgs[i], sigs[i])
		expanded, err := voied25519.NewExpandedPublicKey(pubs[i][:])
		wantVOI := err == nil && voied25519.VerifyExpandedWithOptions(expanded, msgs[i], sigs[i], &voied25519.Options{Verify: voied25519.VerifyOptionsStdLib})
		require.Equalf(t, wantStdlib, wantVOI, "VOI case %d", i)
		require.Equalf(t, wantVOI, got[i], "Narya case %d", i)
	}
}

func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
