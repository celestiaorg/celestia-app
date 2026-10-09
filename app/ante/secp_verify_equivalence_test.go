//go:build cgo && !gofuzz

package ante

import (
	"crypto/sha256"
	"math/big"
	"math/rand"
	"testing"

	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	nativesecp "github.com/ethereum/go-ethereum/crypto/secp256k1"
	"github.com/stretchr/testify/require"
)

// TestNativeSecp256k1AcceptanceIsSubsetOfSDK asserts that a cgo build accepts
// exactly what a nocgo build accepts. The native verifier is tried first and
// the SDK verifier is the fallback, so the two builds agree iff every native
// acceptance is also an SDK acceptance.
func TestNativeSecp256k1AcceptanceIsSubsetOfSDK(t *testing.T) {
	order, ok := new(big.Int).SetString("fffffffffffffffffffffffffffffffebaaedce6af48a03bbfd25e8cd0364141", 16)
	require.True(t, ok)
	halfOrder := new(big.Int).Rsh(order, 1)

	sdkAccepts := func(key *secp256k1.PubKey, msg, sig []byte) bool {
		return key.VerifySignature(msg, sig)
	}
	// The raw native predicate, used only to classify a result. What the union
	// below asserts is the production wrapper, so the guard inside
	// txVerificationPubKey is under test rather than a copy of it.
	nativeAccepts := func(key *secp256k1.PubKey, msg, sig []byte) bool {
		if len(key.Key) != secp256k1.PubKeySize || (key.Key[0] != 2 && key.Key[0] != 3) {
			return false
		}
		hash := sha256.Sum256(msg)
		return nativesecp.VerifySignature(key.Key, hash[:], sig)
	}
	unionAccepts := func(key *secp256k1.PubKey, msg, sig []byte) bool {
		return txVerificationPubKey(key).VerifySignature(msg, sig)
	}

	// setScalar writes v into one half of a signature. A value that needs more
	// than 32 bytes cannot be encoded in a compact signature at all, so those
	// variants are skipped rather than truncated.
	setScalar := func(sig []byte, half int, v *big.Int) ([]byte, bool) {
		if v.BitLen() > 256 || v.Sign() < 0 {
			return nil, false
		}
		out := append([]byte(nil), sig...)
		v.FillBytes(out[half*32 : half*32+32])
		return out, true
	}

	rng := rand.New(rand.NewSource(11))
	var nativeOnly, sdkOnly int

	for i := range 256 {
		priv := secp256k1.GenPrivKey()
		pub := priv.PubKey().(*secp256k1.PubKey)
		msg := make([]byte, 64)
		_, err := rng.Read(msg)
		require.NoError(t, err)
		sig, err := priv.Sign(msg)
		require.NoError(t, err)

		r := new(big.Int).SetBytes(sig[:32])
		s := new(big.Int).SetBytes(sig[32:])

		variants := map[string][]byte{
			"valid": sig,
			"zero":  make([]byte, 64),
			"short": sig[:63],
			"long":  append(append([]byte(nil), sig...), 0),
			"empty": {},
		}
		for name, spec := range map[string]struct {
			half int
			v    *big.Int
		}{
			"high s":     {1, new(big.Int).Sub(order, s)},
			"s = half":   {1, halfOrder},
			"s = half+1": {1, new(big.Int).Add(halfOrder, big.NewInt(1))},
			"s = order":  {1, order},
			"r = order":  {0, order},
			"r + order":  {0, new(big.Int).Add(r, order)},
			"s + order":  {1, new(big.Int).Add(s, order)},
			"r zero":     {0, big.NewInt(0)},
			"s zero":     {1, big.NewInt(0)},
		} {
			if v, ok := setScalar(sig, spec.half, spec.v); ok {
				variants[name] = v
			}
		}

		keys := map[string]*secp256k1.PubKey{
			"ok": pub,
		}
		for _, prefix := range []byte{0x00, 0x01, 0x04, 0x06, 0x07} {
			bad := append([]byte(nil), pub.Key...)
			bad[0] = prefix
			keys["prefix "+string(rune('0'+prefix))] = &secp256k1.PubKey{Key: bad}
		}
		offCurve := append([]byte(nil), pub.Key...)
		offCurve[16] ^= 0xff
		keys["off curve"] = &secp256k1.PubKey{Key: offCurve}
		keys["short key"] = &secp256k1.PubKey{Key: pub.Key[:32]}
		keys["empty key"] = &secp256k1.PubKey{Key: nil}
		keys["zero key"] = &secp256k1.PubKey{Key: make([]byte, secp256k1.PubKeySize)}

		for keyName, key := range keys {
			for sigName, candidate := range variants {
				var native, sdk bool
				require.NotPanics(t, func() {
					native = nativeAccepts(key, msg, candidate)
					sdk = sdkAccepts(key, msg, candidate)
				}, "iteration %d, key %q, signature %q", i, keyName, sigName)

				require.False(t, native && !sdk,
					"native accepted what the SDK rejects: iteration %d, key %q, signature %q",
					i, keyName, sigName)
				// What a cgo build actually accepts is the union, and it must
				// equal what a nocgo build accepts, which is the SDK's set.
				require.Equal(t, sdk, unionAccepts(key, msg, candidate),
					"cgo and nocgo disagree: iteration %d, key %q, signature %q",
					i, keyName, sigName)
				switch {
				case native && !sdk:
					nativeOnly++
				case sdk && !native:
					sdkOnly++
				}
			}
		}
	}

	require.Zero(t, nativeOnly)
	t.Logf("sdk-only acceptances (expected, covered by the fallback): %d", sdkOnly)
}
