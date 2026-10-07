package validator_test

import (
	"crypto/ed25519"
	"math/rand"
	"testing"

	voied25519 "github.com/oasisprotocol/curve25519-voi/primitives/ed25519"
	"github.com/stretchr/testify/require"
)

// The certificate path must retain Go's Ed25519 acceptance rules, including
// behavior on malformed public keys and signatures.
func TestEd25519VerifierMatchesStdLib(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	opts := &voied25519.Options{Verify: voied25519.VerifyOptionsStdLib}
	verify := func(pub, msg, sig []byte) {
		t.Helper()
		want := ed25519.Verify(pub, msg, sig)
		require.Equal(t, want, voied25519.VerifyWithOptions(pub, msg, sig, opts))
		expanded, err := voied25519.NewExpandedPublicKey(pub)
		if err != nil {
			require.False(t, want)
		} else {
			require.Equal(t, want, voied25519.VerifyExpandedWithOptions(expanded, msg, sig, opts))
		}
	}
	for range 256 {
		seed := make([]byte, ed25519.SeedSize)
		_, err := rng.Read(seed)
		require.NoError(t, err)
		priv := ed25519.NewKeyFromSeed(seed)
		pub := priv.Public().(ed25519.PublicKey)
		msg := make([]byte, 100)
		_, err = rng.Read(msg)
		require.NoError(t, err)
		sig := ed25519.Sign(priv, msg)
		verify(pub, msg, sig)
		sig[0] ^= 1
		verify(pub, msg, sig)
		_, err = rng.Read(sig)
		require.NoError(t, err)
		verify(pub, msg, sig)
		_, err = rng.Read(pub)
		require.NoError(t, err)
		verify(pub, msg, sig)
	}
	verify(make([]byte, 32), []byte("message"), make([]byte, 64))
}
