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

// The vectors below are built with plain affine arithmetic over math/big, so
// their construction trusts neither verifier under test. nil is the point at
// infinity.
var (
	curveP    = nativesecp.S256().P
	curveN    = nativesecp.S256().N
	curveHalf = new(big.Int).Rsh(curveN, 1) // (N-1)/2, the largest low s
	curveG    = &point{nativesecp.S256().Gx, nativesecp.S256().Gy}
)

type point struct{ x, y *big.Int }

func addPoints(a, b *point) *point {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	var num, den *big.Int
	if a.x.Cmp(b.x) == 0 {
		if new(big.Int).Add(a.y, b.y).Mod(new(big.Int).Add(a.y, b.y), curveP).Sign() == 0 {
			return nil
		}
		num = new(big.Int).Mul(big.NewInt(3), new(big.Int).Mul(a.x, a.x))
		den = new(big.Int).Lsh(a.y, 1)
	} else {
		num = new(big.Int).Sub(b.y, a.y)
		den = new(big.Int).Sub(b.x, a.x)
	}
	den.Mod(den, curveP)
	l := num.Mul(num, den.ModInverse(den, curveP))
	l.Mod(l, curveP)
	x := new(big.Int).Mul(l, l)
	x.Sub(x, a.x).Sub(x, b.x).Mod(x, curveP)
	y := new(big.Int).Sub(a.x, x)
	y.Mul(y, l).Sub(y, a.y).Mod(y, curveP)
	return &point{x, y}
}

func mulPoint(k *big.Int, a *point) *point {
	k = new(big.Int).Mod(k, curveN)
	var r *point
	for i := k.BitLen() - 1; i >= 0; i-- {
		r = addPoints(r, r)
		if k.Bit(i) == 1 {
			r = addPoints(r, a)
		}
	}
	return r
}

// liftX returns a curve point with x-coordinate x, or nil if there is none.
func liftX(x *big.Int) *point {
	if x.Cmp(curveP) >= 0 {
		return nil
	}
	rhs := new(big.Int).Exp(x, big.NewInt(3), curveP)
	rhs.Add(rhs, big.NewInt(7)).Mod(rhs, curveP)
	y := new(big.Int).ModSqrt(rhs, curveP)
	if y == nil {
		return nil
	}
	return &point{new(big.Int).Set(x), y}
}

func compress(q *point) []byte {
	out := make([]byte, 33)
	out[0] = 2 + byte(q.y.Bit(0))
	q.x.FillBytes(out[1:])
	return out
}

func uncompressed(q *point) []byte {
	out := make([]byte, 65)
	out[0] = 4
	q.x.FillBytes(out[1:33])
	q.y.FillBytes(out[33:])
	return out
}

func modN(v *big.Int) *big.Int    { return new(big.Int).Mod(v, curveN) }
func invN(v *big.Int) *big.Int    { return new(big.Int).ModInverse(v, curveN) }
func mulN(a, b *big.Int) *big.Int { return modN(new(big.Int).Mul(a, b)) }

func msgScalar(msg []byte) *big.Int {
	hash := sha256.Sum256(msg)
	return modN(new(big.Int).SetBytes(hash[:]))
}

// keyFor returns the key that (r, s) is a valid ECDSA signature for over e,
// given the nonce point R with x(R) = r mod N: Q = r^-1 (sR - eG).
func keyFor(e, r, s *big.Int, nonce *point) *point {
	ri := invN(r)
	return addPoints(mulPoint(mulN(ri, s), nonce), mulPoint(modN(new(big.Int).Neg(mulN(ri, e))), curveG))
}

func encodeSig(r, s *big.Int) []byte {
	out := make([]byte, 64)
	r.FillBytes(out[:32])
	s.FillBytes(out[32:])
	return out
}

func nativeRawAccepts(key *secp256k1.PubKey, msg, sig []byte) bool {
	if len(key.Key) != secp256k1.PubKeySize || (key.Key[0] != 2 && key.Key[0] != 3) {
		return false
	}
	hash := sha256.Sum256(msg)
	return nativesecp.VerifySignature(key.Key, hash[:], sig)
}

// checkNativeSubset asserts native ⊆ SDK and cgo == nocgo for one input and
// returns both raw verdicts.
func checkNativeSubset(t testing.TB, key *secp256k1.PubKey, msg, sig []byte, input string) (native, sdk bool) {
	t.Helper()
	require.NotPanics(t, func() {
		native = nativeRawAccepts(key, msg, sig)
		sdk = key.VerifySignature(msg, sig)
	}, input)
	require.False(t, native && !sdk, "native accepted what the SDK rejects: %s", input)
	require.Equal(t, sdk, txVerificationPubKey(key).VerifySignature(msg, sig), "cgo and nocgo disagree: %s", input)
	return native, sdk
}

// TestNativeSecp256k1AdversarialVectors pins both verdicts on inputs built to
// sit on every edge where libsecp256k1 and the SDK's decred verifier could
// differ: the x(R) >= N case of the r comparison, the low-s boundary, the
// largest r and smallest s, a verification sum that doubles or cancels,
// non-canonical key and scalar encodings, and keys native never sees.
func TestNativeSecp256k1AdversarialVectors(t *testing.T) {
	rng := rand.New(rand.NewSource(6))
	randMsg := func() []byte {
		msg := make([]byte, 48)
		_, _ = rng.Read(msg)
		return msg
	}
	randLowS := func() *big.Int {
		return new(big.Int).Add(new(big.Int).Rand(rng, new(big.Int).Sub(curveHalf, big.NewInt(1))), big.NewInt(1))
	}

	type vector struct {
		name                string
		key                 []byte
		msg, sig            []byte
		wantNative, wantSDK bool
	}
	var vectors []vector
	add := func(name string, q *point, msg, sig []byte, wantNative, wantSDK bool) {
		require.NotNil(t, q, name)
		vectors = append(vectors, vector{name, compress(q), msg, sig, wantNative, wantSDK})
	}

	// x(R) in [N, P): r = x(R) - N, so the verifiers must take the r + N branch.
	// Anyone can build these for a key they choose.
	var bigX *point
	for k := int64(1); bigX == nil; k++ {
		bigX = liftX(new(big.Int).Add(curveN, big.NewInt(k)))
	}
	for i := range 8 {
		msg, s := randMsg(), randLowS()
		r := new(big.Int).Sub(bigX.x, curveN)
		nonce := bigX
		if i%2 == 1 {
			nonce = &point{bigX.x, new(big.Int).Sub(curveP, bigX.y)}
		}
		q := keyFor(msgScalar(msg), r, s, nonce)
		add("x(R) >= N", q, msg, encodeSig(r, s), true, true)
		add("x(R) >= N, r encoded as x(R)", q, msg, encodeSig(bigX.x, s), false, true)
		add("x(R) >= N, high s", q, msg, encodeSig(r, new(big.Int).Sub(curveN, s)), false, false)
		add("x(R) >= N, r + 1", q, msg, encodeSig(new(big.Int).Add(r, big.NewInt(1)), s), false, false)
	}

	for range 8 {
		// s exactly (N-1)/2 is low; the same equation with s = (N+1)/2 is a
		// valid but high signature.
		msg := randMsg()
		e := msgScalar(msg)
		nonce := mulPoint(new(big.Int).Rand(rng, curveN), curveG)
		r := modN(nonce.x)
		add("s = (N-1)/2", keyFor(e, r, curveHalf, nonce), msg, encodeSig(r, curveHalf), true, true)
		high := new(big.Int).Add(curveHalf, big.NewInt(1))
		qHigh := keyFor(e, r, high, nonce)
		add("s = (N+1)/2", qHigh, msg, encodeSig(r, high), false, false)
		// Its low-s twin (-R has the same x) verifies.
		add("s = (N+1)/2 negated", qHigh, msg, encodeSig(r, new(big.Int).Sub(curveN, high)), true, true)

		// The verification sum u1*G + u2*Q doubles (u1*G == u2*Q).
		s := randLowS()
		nonce = mulPoint(mulN(big.NewInt(2), mulN(e, invN(s))), curveG)
		r = modN(nonce.x)
		add("u1*G == u2*Q", mulPoint(mulN(e, invN(r)), curveG), msg, encodeSig(r, s), true, true)

		// The verification sum cancels to infinity (u1*G == -u2*Q).
		r = new(big.Int).Add(new(big.Int).Rand(rng, new(big.Int).Sub(curveN, big.NewInt(1))), big.NewInt(1))
		add("u1*G == -u2*Q", mulPoint(modN(new(big.Int).Neg(mulN(e, invN(r)))), curveG), msg, encodeSig(r, s), false, false)
	}

	// Largest canonical r with the smallest s.
	var top *point
	for j := int64(1); top == nil; j++ {
		top = liftX(new(big.Int).Sub(curveN, big.NewInt(j)))
	}
	msg := randMsg()
	add("largest r, s = 1", keyFor(msgScalar(msg), top.x, big.NewInt(1), top), msg, encodeSig(top.x, big.NewInt(1)), true, true)

	for _, v := range vectors {
		native, sdk := checkNativeSubset(t, &secp256k1.PubKey{Key: v.key}, v.msg, v.sig, v.name)
		require.Equal(t, v.wantNative, native, "native: %s", v.name)
		require.Equal(t, v.wantSDK, sdk, "sdk: %s", v.name)
	}

	// A compressed key whose x is P + k, a non-canonical encoding of the point
	// with x = k. Its discrete log is unknown, so no signature can be built;
	// what matters is that native's parser refuses it before any equation.
	var small *point
	k := int64(1)
	for ; small == nil; k++ {
		small = liftX(big.NewInt(k))
	}
	overP := make([]byte, 33)
	overP[0] = 2 + byte(small.y.Bit(0))
	new(big.Int).Add(curveP, small.x).FillBytes(overP[1:])
	x, _ := nativesecp.DecompressPubkey(overP)
	require.Nil(t, x, "native parses a key with x >= P")
	require.NotNil(t, func() *big.Int { x, _ := nativesecp.DecompressPubkey(compress(small)); return x }())

	// Keys native never sees (65-byte encodings) are the SDK's alone, so the
	// union still accepts what the SDK does.
	priv := secp256k1.GenPrivKeyFromSecret([]byte("uncompressed"))
	msg = randMsg()
	sig, err := priv.Sign(msg)
	require.NoError(t, err)
	px, py := nativesecp.DecompressPubkey(priv.PubKey().Bytes())
	native, sdk := checkNativeSubset(t, &secp256k1.PubKey{Key: uncompressed(&point{px, py})}, msg, sig, "uncompressed key")
	require.False(t, native)
	require.True(t, sdk)
}

// TestNativeSecp256k1RandomMutations checks the property on random valid
// signatures and on single-bit corruptions of every input.
func TestNativeSecp256k1RandomMutations(t *testing.T) {
	rng := rand.New(rand.NewSource(66))
	flip := func(b []byte) []byte {
		out := append([]byte(nil), b...)
		i := rng.Intn(len(out) * 8)
		out[i/8] ^= 1 << (i % 8)
		return out
	}
	accepted := 0
	for i := range 2000 {
		secret := make([]byte, 32)
		_, _ = rng.Read(secret)
		priv := secp256k1.GenPrivKeyFromSecret(secret)
		key := priv.PubKey().(*secp256k1.PubKey)
		msg := make([]byte, 1+rng.Intn(256))
		_, _ = rng.Read(msg)
		sig, err := priv.Sign(msg)
		require.NoError(t, err)

		if native, _ := checkNativeSubset(t, key, msg, sig, "valid"); native {
			accepted++
		}
		s := new(big.Int).SetBytes(sig[32:])
		checkNativeSubset(t, key, msg, encodeSig(new(big.Int).SetBytes(sig[:32]), new(big.Int).Sub(curveN, s)), "high s")
		checkNativeSubset(t, key, msg, flip(sig), "sig bit flip")
		checkNativeSubset(t, key, flip(msg), sig, "msg bit flip")
		checkNativeSubset(t, &secp256k1.PubKey{Key: flip(key.Key)}, msg, sig, "key bit flip")
		garbage := make([]byte, 64)
		_, _ = rng.Read(garbage)
		checkNativeSubset(t, key, msg, garbage, "random sig")
		randKey := make([]byte, 33)
		_, _ = rng.Read(randKey)
		randKey[0] = 2 + byte(i%2)
		checkNativeSubset(t, &secp256k1.PubKey{Key: randKey}, msg, sig, "random key")
	}
	require.Equal(t, 2000, accepted, "native rejected a valid low-s signature")
}

// FuzzNativeSecp256k1SubsetOfSDK searches for an input native accepts and the
// SDK rejects. `go test -run '^$' -fuzz FuzzNativeSecp256k1SubsetOfSDK ./app/ante`
func FuzzNativeSecp256k1SubsetOfSDK(f *testing.F) {
	priv := secp256k1.GenPrivKeyFromSecret([]byte("fuzz"))
	msg := []byte("fibre")
	sig, err := priv.Sign(msg)
	require.NoError(f, err)
	f.Add(priv.PubKey().Bytes(), msg, sig)
	f.Add(priv.PubKey().Bytes(), msg, encodeSig(new(big.Int).SetBytes(sig[:32]), new(big.Int).Sub(curveN, new(big.Int).SetBytes(sig[32:]))))
	f.Fuzz(func(t *testing.T, key, msg, sig []byte) {
		checkNativeSubset(t, &secp256k1.PubKey{Key: key}, msg, sig, "fuzz")
	})
}
