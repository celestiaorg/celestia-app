package validator

import (
	"testing"

	"github.com/cometbft/cometbft/crypto"
	"github.com/cometbft/cometbft/crypto/ed25519"
	"github.com/stretchr/testify/require"
)

func TestSignatureEvidenceExactInputs(t *testing.T) {
	key := ed25519.GenPrivKeyFromSecret([]byte("evidence"))
	message := []byte("promise")
	sig, err := key.Sign(message)
	require.NoError(t, err)
	e := &SignatureEvidence{}
	require.False(t, e.has(message, key.PubKey(), sig))
	e.RecordVerified(message, []crypto.PubKey{key.PubKey()}, [][]byte{sig})
	require.True(t, e.has(message, key.PubKey(), sig))
	require.False(t, e.has([]byte("different"), key.PubKey(), sig))
	require.False(t, e.has(message, ed25519.GenPrivKeyFromSecret([]byte("other")).PubKey(), sig))
	sig[0] ^= 1
	require.False(t, e.has(message, key.PubKey(), sig), "mutating a signature cannot mutate its evidence")
	require.False(t, (&SignatureEvidence{}).has(message, key.PubKey(), sig), "evidence is call-local")
}
