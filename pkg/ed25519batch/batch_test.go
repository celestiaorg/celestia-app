package ed25519batch_test

import (
	"crypto/rand"
	"encoding/hex"
	mathrand "math/rand"
	"testing"

	"github.com/celestiaorg/celestia-app/v10/pkg/ed25519batch"
	"github.com/cometbft/cometbft/crypto/ed25519"
	"github.com/stretchr/testify/require"
)

// smallOrderPoint is a canonical encoding of a point of order 8. ZIP-215
// accepts signatures built from it that the cofactorless equation rejects, so
// it is the sharpest available probe of whether the batch verifier and the
// single verifier share an acceptance rule.
const smallOrderPoint = "26e8958fc2b227b045c3f489f2ef98f0d5dfac05d3c63339b13802886d53fc05"

// TestBatchAgreesWithSingleVerifier is the correctness gate for batching.
//
// A batch that accepts a signature the single verifier rejects would let a
// cache-warm node accept a certificate a cache-cold node rejects - a consensus
// fault. This asserts the two decide identically over honest, tampered,
// malformed and adversarial inputs, and that a batch accepts only when every
// member does.
func TestBatchAgreesWithSingleVerifier(t *testing.T) {
	rng := mathrand.New(mathrand.NewSource(3))
	smallOrder, err := hex.DecodeString(smallOrderPoint)
	require.NoError(t, err)

	type signature struct {
		key ed25519.PubKey
		msg []byte
		sig []byte
	}

	// singleAccepts is the predicate the ordered walk applies.
	singleAccepts := func(s signature) bool { return s.key.VerifySignature(s.msg, s.sig) }

	batchAccepts := func(sigs []signature) bool {
		v := new(ed25519batch.Verifier)
		for _, s := range sigs {
			_ = v.Add(s.key, s.msg, s.sig)
		}
		return v.VerifyBatchOnly(rand.Reader)
	}

	corpus := make([]signature, 0, 512)
	for range 128 {
		priv := ed25519.GenPrivKey()
		pub := priv.PubKey().(ed25519.PubKey)
		msg := make([]byte, 32)
		_, err := rng.Read(msg)
		require.NoError(t, err)
		sig, err := priv.Sign(msg)
		require.NoError(t, err)

		corpus = append(corpus, signature{pub, msg, sig})

		tampered := append([]byte(nil), sig...)
		tampered[rng.Intn(len(tampered))] ^= 1 << uint(rng.Intn(8))
		corpus = append(corpus, signature{pub, msg, tampered})

		random := make([]byte, 64)
		_, err = rng.Read(random)
		require.NoError(t, err)
		corpus = append(corpus, signature{pub, msg, random})

		// a small-order key with R = identity and s = 0: ZIP-215 accepts it
		zip215Only := make([]byte, 64)
		zip215Only[0] = 1
		corpus = append(corpus, signature{ed25519.PubKey(smallOrder), msg, zip215Only})
	}

	// every member decides the same way in a batch of one as on its own
	agreed := 0
	for i, s := range corpus {
		single := singleAccepts(s)
		require.Equal(t, single, batchAccepts([]signature{s}),
			"singleton batch disagreed with the single verifier at %d", i)
		if single {
			agreed++
		}
	}
	require.NotZero(t, agreed, "the corpus must contain accepted signatures")

	// a batch accepts exactly when every member does
	for range 400 {
		n := 1 + rng.Intn(12)
		members := make([]signature, n)
		allValid := true
		for i := range members {
			members[i] = corpus[rng.Intn(len(corpus))]
			if !singleAccepts(members[i]) {
				allValid = false
			}
		}
		require.Equal(t, allValid, batchAccepts(members),
			"batch of %d disagreed with the conjunction of its members", n)
	}
}

// TestBatchRepeatedKeysCannotCancel pins the aggregation of repeated public
// keys: combining coefficients for one key must not let a forged signature
// cancel against a valid one.
func TestBatchRepeatedKeysCannotCancel(t *testing.T) {
	priv := ed25519.GenPrivKey()
	pub := priv.PubKey().(ed25519.PubKey)
	msg := []byte("a message signed once")
	valid, err := priv.Sign(msg)
	require.NoError(t, err)

	forged := append([]byte(nil), valid...)
	forged[0] ^= 0xff

	for range 200 {
		v := new(ed25519batch.Verifier)
		require.NoError(t, v.Add(pub, msg, valid))
		_ = v.Add(pub, msg, forged)
		require.False(t, v.VerifyBatchOnly(rand.Reader),
			"a forged signature must not cancel against a valid one under the same key")
	}
}

// TestBatchResetReusesVerifier pins that a reused Verifier does not carry
// state between batches in either direction.
func TestBatchResetReusesVerifier(t *testing.T) {
	priv := ed25519.GenPrivKey()
	pub := priv.PubKey().(ed25519.PubKey)
	msg := []byte("reuse")
	valid, err := priv.Sign(msg)
	require.NoError(t, err)
	invalid := append([]byte(nil), valid...)
	invalid[10] ^= 1

	v := new(ed25519batch.Verifier)
	for range 50 {
		v.Reset()
		require.NoError(t, v.Add(pub, msg, valid))
		require.True(t, v.VerifyBatchOnly(rand.Reader))

		v.Reset()
		_ = v.Add(pub, msg, invalid)
		require.False(t, v.VerifyBatchOnly(rand.Reader))
	}
}
