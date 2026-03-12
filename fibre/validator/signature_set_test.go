package validator_test

import (
	"sync"
	"testing"

	"github.com/celestiaorg/celestia-app/v8/fibre/validator"
	"github.com/cometbft/cometbft/crypto/ed25519"
	cmtmath "github.com/cometbft/cometbft/libs/math"
	core "github.com/cometbft/cometbft/types"
	"github.com/stretchr/testify/require"
)

// makeValidators creates n validators with the given voting power each
func makeValidators(n int, votingPower int64) ([]*core.Validator, []ed25519.PrivKey) {
	validators := make([]*core.Validator, n)
	privKeys := make([]ed25519.PrivKey, n)
	for i := range n {
		privKeys[i] = ed25519.GenPrivKey()
		validators[i] = core.NewValidator(privKeys[i].PubKey(), votingPower)
	}
	return validators, privKeys
}

type testSetup struct {
	validators []*core.Validator
	privKeys   []ed25519.PrivKey
	valSet     validator.Set
	sigSet     *validator.SignatureSet
	signBytes  []byte
}

func setupSignatureSet(numVals int, votingPower int64, votingPowerFrac cmtmath.Fraction) *testSetup {
	signBytes := []byte("test message to sign")
	validators, privKeys := makeValidators(numVals, votingPower)
	valSet := validator.Set{
		ValidatorSet: core.NewValidatorSet(validators),
		Height:       100,
	}
	sigSet := valSet.NewSignatureSet(votingPowerFrac, signBytes)

	return &testSetup{
		validators: validators,
		privKeys:   privKeys,
		valSet:     valSet,
		sigSet:     sigSet,
		signBytes:  signBytes,
	}
}

func TestSignatureSet(t *testing.T) {
	twoThirds := cmtmath.Fraction{Numerator: 2, Denominator: 3}
	half := cmtmath.Fraction{Numerator: 1, Denominator: 2}

	t.Run("NotEnoughVotingPower", func(t *testing.T) {
		s := setupSignatureSet(5, 10, twoThirds)

		// 5 validators * 10 voting power = 50 total
		// 2/3 of 50 = 33 requiredVotingPower
		// 2/3 of 5 = 3 requiredCount
		// Add 3 signatures (30 voting power, meets count threshold of 3 but not voting power threshold of 33)
		for i := range 3 {
			signature, err := s.privKeys[i].Sign(s.signBytes)
			require.NoError(t, err)
			hasEnough, err := s.sigSet.Add(s.validators[i], signature)
			require.NoError(t, err)
			require.False(t, hasEnough)
		}

		sigs, err := s.sigSet.Signatures()
		require.Nil(t, sigs)
		require.Error(t, err)

		var sigErr *validator.NotEnoughSignaturesError
		require.ErrorAs(t, err, &sigErr)
		require.Len(t, sigErr.Collected, 5)
		nonNil := 0
		for _, sig := range sigErr.Collected {
			if sig != nil {
				nonNil++
			}
		}
		require.Equal(t, 3, nonNil)
		require.Equal(t, int64(30), sigErr.CollectedPower)
		require.Equal(t, int64(33), sigErr.RequiredPower)
		require.Contains(t, err.Error(), "not enough voting power")
	})

	t.Run("NotEnoughSignatures", func(t *testing.T) {
		s := setupSignatureSet(5, 10, twoThirds)

		// Add only 2 signatures (requiredCount = 3)
		for i := range 2 {
			signature, err := s.privKeys[i].Sign(s.signBytes)
			require.NoError(t, err)
			hasEnough, err := s.sigSet.Add(s.validators[i], signature)
			require.NoError(t, err)
			require.False(t, hasEnough)
		}

		sigs, err := s.sigSet.Signatures()
		require.Nil(t, sigs)
		require.Error(t, err)

		var sigErr *validator.NotEnoughSignaturesError
		require.ErrorAs(t, err, &sigErr)
		require.Len(t, sigErr.Collected, 5)
		nonNil := 0
		for _, sig := range sigErr.Collected {
			if sig != nil {
				nonNil++
			}
		}
		require.Equal(t, 2, nonNil)
		require.Equal(t, int64(20), sigErr.CollectedPower)
		require.Equal(t, int64(33), sigErr.RequiredPower)
		require.Contains(t, err.Error(), "not enough voting power")
	})

	t.Run("SuccessSequential", func(t *testing.T) {
		s := setupSignatureSet(5, 10, twoThirds)

		// Add 4 signatures (40 voting power, meets both thresholds)
		for i := range 4 {
			signature, err := s.privKeys[i].Sign(s.signBytes)
			require.NoError(t, err)
			_, err = s.sigSet.Add(s.validators[i], signature)
			require.NoError(t, err)
		}

		sigs, err := s.sigSet.Signatures()
		require.NoError(t, err)
		require.Len(t, sigs, 5)
		nonNil := 0
		for _, sig := range sigs {
			if sig != nil {
				nonNil++
			}
		}
		require.Equal(t, 4, nonNil)
	})

	t.Run("SuccessConcurrent", func(t *testing.T) {
		s := setupSignatureSet(10, 10, twoThirds)

		var wg sync.WaitGroup
		for i := range 10 {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				signature, err := s.privKeys[idx].Sign(s.signBytes)
				require.NoError(t, err)
				_, err = s.sigSet.Add(s.validators[idx], signature)
				require.NoError(t, err)
			}(i)
		}
		wg.Wait()

		sigs, err := s.sigSet.Signatures()
		require.NoError(t, err)
		require.Len(t, sigs, 10)
	})

	t.Run("InvalidSignature", func(t *testing.T) {
		s := setupSignatureSet(3, 10, half)

		wrongSignBytes := []byte("wrong message")
		signature, err := s.privKeys[0].Sign(wrongSignBytes)
		require.NoError(t, err)

		hasEnough, err := s.sigSet.Add(s.validators[0], signature)
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid signature")
		require.False(t, hasEnough)
	})

	t.Run("DuplicateSignatureNoOp", func(t *testing.T) {
		s := setupSignatureSet(3, 10, half)

		// Add a valid signature from validator 0
		signature, err := s.privKeys[0].Sign(s.signBytes)
		require.NoError(t, err)
		hasEnough, err := s.sigSet.Add(s.validators[0], signature)
		require.NoError(t, err)
		require.False(t, hasEnough)

		// Add the same validator's signature again — should be a no-op
		hasEnough, err = s.sigSet.Add(s.validators[0], signature)
		require.NoError(t, err)
		require.False(t, hasEnough)

		// Voting power should be 10 (not 20)
		sigs, err := s.sigSet.Signatures()
		require.Error(t, err)

		var sigErr *validator.NotEnoughSignaturesError
		require.ErrorAs(t, err, &sigErr)
		require.Equal(t, int64(10), sigErr.CollectedPower)
		require.Equal(t, int64(15), sigErr.RequiredPower)
		require.Nil(t, sigs)
	})

	t.Run("DuplicateSignatureConcurrent", func(t *testing.T) {
		s := setupSignatureSet(3, 10, half)

		signature, err := s.privKeys[0].Sign(s.signBytes)
		require.NoError(t, err)

		// Add the same validator's signature concurrently from 10 goroutines
		var wg sync.WaitGroup
		for range 10 {
			wg.Go(func() {
				_, addErr := s.sigSet.Add(s.validators[0], signature)
				require.NoError(t, addErr)
			})
		}
		wg.Wait()

		// Voting power should be 10 (not 100)
		_, err = s.sigSet.Signatures()
		require.Error(t, err)

		var sigErr *validator.NotEnoughSignaturesError
		require.ErrorAs(t, err, &sigErr)
		require.Equal(t, int64(10), sigErr.CollectedPower)
	})

	t.Run("MixedMissAndValid", func(t *testing.T) {
		s := setupSignatureSet(5, 10, half)

		// Add 3 valid signatures (30 voting power, meets threshold of 25)
		for i := range 3 {
			signature, err := s.privKeys[i].Sign(s.signBytes)
			require.NoError(t, err)
			_, err = s.sigSet.Add(s.validators[i], signature)
			require.NoError(t, err)
		}

		sigs, err := s.sigSet.Signatures()
		require.NoError(t, err)
		require.Len(t, sigs, 5)
		nonNil := 0
		for _, sig := range sigs {
			if sig != nil {
				nonNil++
			}
		}
		require.Equal(t, 3, nonNil)
	})
}
