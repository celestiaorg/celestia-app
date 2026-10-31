package secpverify

import (
	"testing"

	"github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	"github.com/stretchr/testify/require"
)

func TestPubKey(t *testing.T) {
	ed := ed25519.GenPrivKey().PubKey()
	require.Same(t, ed, PubKey(ed))

	priv := secp256k1.GenPrivKey()
	pk := priv.PubKey().(*secp256k1.PubKey)
	wrapped := PubKey(pk)
	if Native {
		require.Equal(t, pubKey{pk}, wrapped)
	} else {
		require.Same(t, pk, wrapped)
	}
	require.Equal(t, pk.Address(), wrapped.Address())
	require.True(t, wrapped.Equals(pk))
	sig, err := priv.Sign([]byte("msg"))
	require.NoError(t, err)
	require.True(t, wrapped.VerifySignature([]byte("msg"), sig))
	require.False(t, wrapped.VerifySignature([]byte("msh"), sig))
}

func BenchmarkVerify(b *testing.B) {
	priv := secp256k1.GenPrivKey()
	pk := priv.PubKey().(*secp256k1.PubKey)
	msg := make([]byte, 300)
	sig, err := priv.Sign(msg)
	require.NoError(b, err)
	b.Run("decred", func(b *testing.B) {
		for b.Loop() {
			if !pk.VerifySignature(msg, sig) {
				b.Fatal("rejected")
			}
		}
	})
	b.Run("secpverify", func(b *testing.B) {
		for b.Loop() {
			if !Verify(pk, msg, sig) {
				b.Fatal("rejected")
			}
		}
	})
}
