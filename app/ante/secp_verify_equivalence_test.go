//go:build cgo && !gofuzz

package ante

import (
	"crypto/sha256"
	"fmt"
	"math/big"
	"math/rand"
	"testing"

	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	nativesecp "github.com/ethereum/go-ethereum/crypto/secp256k1"
	"github.com/stretchr/testify/require"
)

// TestNativeSecp256k1AcceptanceIsSubsetOfSDK checks a cgo build accepts exactly
// what a nocgo build accepts: every native acceptance is also an SDK one.
func TestNativeSecp256k1AcceptanceIsSubsetOfSDK(t *testing.T) {
	order := nativesecp.S256().N
	halfOrder := new(big.Int).Rsh(order, 1)

	sdkAccepts := func(key *secp256k1.PubKey, msg, sig []byte) bool {
		return key.VerifySignature(msg, sig)
	}
	// The raw native predicate, used only to classify a result; the union below
	// asserts the production wrapper.
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

	// setScalar writes v into one half of a signature. Values needing more than
	// 32 bytes cannot be encoded compactly, so they are skipped.
	setScalar := func(sig []byte, half int, v *big.Int) ([]byte, bool) {
		if v.BitLen() > 256 || v.Sign() < 0 {
			return nil, false
		}
		out := append([]byte(nil), sig...)
		v.FillBytes(out[half*32 : half*32+32])
		return out, true
	}

	var sdkOnly int
	// check asserts the property for one input and counts the inputs only the
	// SDK accepts: the non-canonical encodings the fallback exists for.
	check := func(key *secp256k1.PubKey, msg, sig []byte, input string) {
		t.Helper()
		var native, sdk bool
		require.NotPanics(t, func() {
			native = nativeAccepts(key, msg, sig)
			sdk = sdkAccepts(key, msg, sig)
		}, input)
		require.False(t, native && !sdk, "native accepted what the SDK rejects: %s", input)
		// What a cgo build actually accepts is the union, and it must equal
		// what a nocgo build accepts, which is the SDK's set.
		require.Equal(t, sdk, unionAccepts(key, msg, sig), "cgo and nocgo disagree: %s", input)
		if sdk && !native {
			sdkOnly++
		}
	}

	rng := rand.New(rand.NewSource(11))

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
				check(key, msg, candidate, fmt.Sprintf("iteration %d, key %q, signature %q", i, keyName, sigName))
			}
		}
	}

	// A random signature's r or s plus the order almost never fits in 32
	// bytes, so the loop above cannot reach the non-canonical encodings.
	// Build signatures with small r and s instead: pick (r, s), recover the
	// key they are valid for, then add the order to either scalar.
	nonCanonical := 0
	for r := int64(1); nonCanonical < 16; r++ {
		msg := make([]byte, 64)
		_, err := rng.Read(msg)
		require.NoError(t, err)
		hash := sha256.Sum256(msg)
		recoverable := make([]byte, 65)
		big.NewInt(r).FillBytes(recoverable[:32])
		big.NewInt(r + 1000).FillBytes(recoverable[32:64])
		uncompressed, err := nativesecp.RecoverPubkey(hash[:], recoverable)
		if err != nil {
			continue // r is not the x-coordinate of a curve point
		}
		x := new(big.Int).SetBytes(uncompressed[1:33])
		y := new(big.Int).SetBytes(uncompressed[33:])
		key := &secp256k1.PubKey{Key: nativesecp.CompressPubkey(x, y)}
		sig := recoverable[:64]
		require.True(t, sdkAccepts(key, msg, sig), "constructed signature must verify, r = %d", r)

		for name, spec := range map[string]struct {
			half int
			v    *big.Int
		}{
			"canonical": {0, big.NewInt(r)},
			"r + order": {0, new(big.Int).Add(big.NewInt(r), order)},
			"s + order": {1, new(big.Int).Add(big.NewInt(r+1000), order)},
		} {
			candidate, ok := setScalar(sig, spec.half, spec.v)
			require.True(t, ok)
			check(key, msg, candidate, fmt.Sprintf("r = %d, signature %q", r, name))
		}
		nonCanonical++
	}

	// Two non-canonical encodings per constructed signature, and nothing else
	// is SDK-only: the fallback is exercised, and only where it is meant to be.
	require.Equal(t, 2*nonCanonical, sdkOnly)
}
