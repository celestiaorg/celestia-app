package validator_test

import (
	stded25519 "crypto/ed25519"
	"crypto/sha512"
	"encoding/hex"
	"math/big"
	"math/rand"
	"testing"

	"github.com/celestiaorg/celestia-app/v10/fibre/validator"
	"github.com/cometbft/cometbft/crypto/ed25519"
	cmtmath "github.com/cometbft/cometbft/libs/math"
	core "github.com/cometbft/cometbft/types"
	"github.com/oasisprotocol/curve25519-voi/curve"
	"github.com/oasisprotocol/curve25519-voi/curve/scalar"
	"github.com/stretchr/testify/require"
)

// smallOrderPoint is the canonical encoding of a point of order 8. The tests
// assert that property rather than trusting the constant.
const smallOrderPoint = "26e8958fc2b227b045c3f489f2ef98f0d5dfac05d3c63339b13802886d53fc05"

// nonCanonicalIdentity encodes y = p, a non-canonical spelling of the y = 0
// point of order 4. ZIP-215 decodes it; crypto/ed25519 rejects it.
const nonCanonicalIdentity = "edffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff7f"

var twoThirds = cmtmath.Fraction{Numerator: 2, Denominator: 3}

// accepts reports whether [validator.SignatureSet.Add] accepts signature from a
// single validator holding pubKey.
func accepts(pubKey, signBytes, signature []byte) bool {
	val := core.NewValidator(ed25519.PubKey(pubKey), 10)
	valSet := validator.Set{
		ValidatorSet: core.NewValidatorSet([]*core.Validator{val}),
		Height:       100,
	}
	_, err := valSet.NewSignatureSet(twoThirds, signBytes).Add(val, signature)
	return err == nil
}

func mustDecodeHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	require.NoError(t, err)
	return b
}

// TestAddAgreesWithStdLibOnHonestSignatures checks that ZIP-215 and
// crypto/ed25519 decide alike on signed messages and on corrupted ones. The
// tests that follow cover the cases where they differ.
func TestAddAgreesWithStdLibOnHonestSignatures(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	signBytes := []byte("test message to sign")

	for range 256 {
		seed := make([]byte, stded25519.SeedSize)
		_, err := rng.Read(seed)
		require.NoError(t, err)
		priv := stded25519.NewKeyFromSeed(seed)
		pubKey := []byte(priv.Public().(stded25519.PublicKey))
		signature := stded25519.Sign(priv, signBytes)

		// a signature as produced by signing
		require.True(t, accepts(pubKey, signBytes, signature))
		require.True(t, stded25519.Verify(pubKey, signBytes, signature))

		// a signature with one bit flipped
		tampered := append([]byte(nil), signature...)
		tampered[0] ^= 1
		require.Equal(t, stded25519.Verify(pubKey, signBytes, tampered), accepts(pubKey, signBytes, tampered))

		// random bytes in the signature, then in the public key too
		random := make([]byte, len(signature))
		_, err = rng.Read(random)
		require.NoError(t, err)
		require.Equal(t, stded25519.Verify(pubKey, signBytes, random), accepts(pubKey, signBytes, random))

		randomKey := make([]byte, len(pubKey))
		_, err = rng.Read(randomKey)
		require.NoError(t, err)
		require.Equal(t, stded25519.Verify(randomKey, signBytes, random), accepts(randomKey, signBytes, random))
	}
}

// TestAddAcceptsZIP215OnlySignatureFromOrdinaryKey checks that a validator with
// an ordinary key can mint a signature ZIP-215 accepts and crypto/ed25519
// rejects, by setting R to a point the cofactor kills and s to k*a.
func TestAddAcceptsZIP215OnlySignatureFromOrdinaryKey(t *testing.T) {
	seed := make([]byte, stded25519.SeedSize)
	for i := range seed {
		seed[i] = byte(i)
	}
	priv := stded25519.NewKeyFromSeed(seed)
	pubKey := []byte(priv.Public().(stded25519.PublicKey))

	// the key is an ordinary one: canonical, not small order
	compressed, err := curve.NewCompressedEdwardsYFromBytes(pubKey)
	require.NoError(t, err)
	require.True(t, compressed.IsCanonicalVartime(), "the key must be canonical")
	point, err := curve.NewEdwardsPoint().SetCompressedY(compressed)
	require.NoError(t, err)
	require.False(t, point.IsSmallOrder(), "the key must not be small order")

	// a = the secret scalar behind pubKey, reduced mod the group order
	digest := sha512.Sum512(seed)
	clamped := digest[:32]
	clamped[0] &= 248
	clamped[31] &= 127
	clamped[31] |= 64
	secret, err := scalar.New().SetBytesModOrder(clamped)
	require.NoError(t, err)

	// R = a point killed by the cofactor, s = k*a with k = H(R || A || M)
	r := mustDecodeHex(t, smallOrderPoint)
	signBytes := []byte("test message to sign")

	challenge := sha512.New()
	challenge.Write(r)
	challenge.Write(pubKey)
	challenge.Write(signBytes)
	k, err := scalar.New().SetBytesModOrderWide(challenge.Sum(nil))
	require.NoError(t, err)

	s := make([]byte, 32)
	require.NoError(t, scalar.New().Mul(k, secret).ToBytes(s))

	signature := append(append([]byte(nil), r...), s...)
	require.Len(t, signature, ed25519.SignatureSize)

	require.True(t, accepts(pubKey, signBytes, signature),
		"ZIP-215 accepts a cofactor-killed R from an ordinary key")
	require.False(t, stded25519.Verify(pubKey, signBytes, signature),
		"crypto/ed25519 rejects it - this is the acceptance change")
}

// TestAddAcceptsSmallOrderKey checks the other divergence class: a public key
// the cofactor kills. Such a key cannot carry honest voting power, but every
// node must still decide it alike.
func TestAddAcceptsSmallOrderKey(t *testing.T) {
	pubKey := mustDecodeHex(t, smallOrderPoint)

	compressed, err := curve.NewCompressedEdwardsYFromBytes(pubKey)
	require.NoError(t, err)
	require.True(t, compressed.IsCanonicalVartime())
	point, err := curve.NewEdwardsPoint().SetCompressedY(compressed)
	require.NoError(t, err)
	require.True(t, point.IsSmallOrder(), "must be killed by the cofactor")
	require.False(t, point.IsIdentity(), "must not be the identity")
	// exactly order 8: doubling it twice does not reach the identity
	double := curve.NewEdwardsPoint().Add(point, point)
	require.False(t, curve.NewEdwardsPoint().Add(double, double).IsIdentity(),
		"must have order 8, not 2 or 4")

	// R = identity, s = 0: [8][s]B = [8]R + [8][k]A holds for any message,
	// because [8][k]A is the identity whatever k is.
	signature := make([]byte, ed25519.SignatureSize)
	identity, err := curve.NewCompressedEdwardsY().Identity().MarshalBinary()
	require.NoError(t, err)
	copy(signature, identity)

	signBytes := []byte("test message to sign")
	require.True(t, accepts(pubKey, signBytes, signature))
	require.False(t, stded25519.Verify(pubKey, signBytes, signature))
}

// TestAddAcceptsNonCanonicalKey checks that ZIP-215 decodes a non-canonically
// encoded public key where crypto/ed25519 refuses to.
func TestAddAcceptsNonCanonicalKey(t *testing.T) {
	pubKey := mustDecodeHex(t, nonCanonicalIdentity)
	compressed, err := curve.NewCompressedEdwardsYFromBytes(pubKey)
	require.NoError(t, err)
	require.False(t, compressed.IsCanonicalVartime(), "the vector must be non-canonical")

	signature := make([]byte, ed25519.SignatureSize)
	identity, err := curve.NewCompressedEdwardsY().Identity().MarshalBinary()
	require.NoError(t, err)
	copy(signature, identity)

	signBytes := []byte("test message to sign")
	require.True(t, accepts(pubKey, signBytes, signature))
	require.False(t, stded25519.Verify(pubKey, signBytes, signature))
}

// TestAddAcceptsNonCanonicalSignaturePoint checks the same for a non-canonical
// R. With s = 0 and a key the cofactor kills, both sides of the ZIP-215
// equation are the identity.
func TestAddAcceptsNonCanonicalSignaturePoint(t *testing.T) {
	pubKey := mustDecodeHex(t, smallOrderPoint)
	nonCanonicalR := mustDecodeHex(t, nonCanonicalIdentity)
	compressed, err := curve.NewCompressedEdwardsYFromBytes(nonCanonicalR)
	require.NoError(t, err)
	require.False(t, compressed.IsCanonicalVartime(), "the vector must be non-canonical")

	signature := make([]byte, ed25519.SignatureSize)
	copy(signature, nonCanonicalR)

	signBytes := []byte("test message to sign")
	require.True(t, accepts(pubKey, signBytes, signature))
	require.False(t, stded25519.Verify(pubKey, signBytes, signature))
}

// TestAddRejectsNonMinimalScalar checks the malleability guard both predicates
// keep: s must be reduced modulo the group order.
func TestAddRejectsNonMinimalScalar(t *testing.T) {
	seed := make([]byte, stded25519.SeedSize)
	for i := range seed {
		seed[i] = byte(i + 1)
	}
	priv := stded25519.NewKeyFromSeed(seed)
	pubKey := []byte(priv.Public().(stded25519.PublicKey))
	signBytes := []byte("test message to sign")
	signature := stded25519.Sign(priv, signBytes)
	require.True(t, accepts(pubKey, signBytes, signature))

	// s + L is a different encoding of the same scalar, and is not minimal
	order, ok := new(big.Int).SetString("1000000000000000000000000000000014def9dea2f79cd65812631a5cf5d3ed", 16)
	require.True(t, ok)
	s := new(big.Int).SetBytes(reversed(signature[32:]))
	raised := reversed(new(big.Int).Add(s, order).FillBytes(make([]byte, 32)))

	malleable := append(append([]byte(nil), signature[:32]...), raised...)
	require.False(t, accepts(pubKey, signBytes, malleable))
	require.False(t, stded25519.Verify(pubKey, signBytes, malleable))
}

// TestAddRejectsMalformedSignatures checks that no wrong signature length is
// accepted and none panics. Key length is not covered because CometBFT panics
// deriving the address of a key that is not 32 bytes, before verification.
func TestAddRejectsMalformedSignatures(t *testing.T) {
	seed := make([]byte, stded25519.SeedSize)
	priv := stded25519.NewKeyFromSeed(seed)
	pubKey := []byte(priv.Public().(stded25519.PublicKey))
	signBytes := []byte("test message to sign")
	signature := stded25519.Sign(priv, signBytes)

	for _, length := range []int{0, 1, 63, 65, 128} {
		resized := make([]byte, length)
		copy(resized, signature)
		require.NotPanics(t, func() {
			require.False(t, accepts(pubKey, signBytes, resized), "signature of %d bytes", length)
		})
	}
}

// reversed returns a copy of b in the opposite byte order, converting between
// the little-endian encoding Ed25519 uses and big.Int's big-endian one.
func reversed(b []byte) []byte {
	out := make([]byte, len(b))
	for i, v := range b {
		out[len(b)-1-i] = v
	}
	return out
}
