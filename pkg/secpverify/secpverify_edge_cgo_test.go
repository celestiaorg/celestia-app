//go:build cgo

package secpverify

import (
	"crypto/sha256"
	"math/big"
	"testing"

	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	dcrsecp "github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/stretchr/testify/require"
)

// TestVerifyMatchesDecredVerifyEdges builds signatures, with plain big.Int curve
// arithmetic, that reach the edges of the verification equation itself.
func TestVerifyMatchesDecredVerifyEdges(t *testing.T) {
	g := dcrG()
	check := func(name string, q ecPoint, msg, sig []byte, want bool) {
		t.Helper()
		for j, p := range formats(q.compressed()) {
			got := requireSameVerdict(t, name, p, msg, sig)
			require.Equal(t, want && j != 3, got, "%s (format %d)", name, j)
		}
	}
	var highX, wrapX int
	for i := range 24 {
		msg := []byte{byte(i), 'm'}
		e := msgScalar(msg)

		// x(R) in [N, P): verify takes the second r + N comparison. Encoding r as
		// x(R) itself (>= N) must reduce to the same verdict.
		x := new(big.Int).Add(curveN, big.NewInt(int64(i+1)))
		if R, ok := liftX(x, i%2 == 1); ok {
			r := new(big.Int).Sub(x, curveN)
			s := lowS(big.NewInt(int64(1000 + i)))
			q := recoverKey(R, r, s, e)
			highX++
			check("x(R) >= N", q, msg, encode(r, s), true)
			check("x(R) >= N, R encoded as x(R)", q, msg, encode(x, s), true)
			check("x(R) >= N, high S", q, msg, encode(r, new(big.Int).Sub(curveN, s)), false)
		}

		// x(R) = r + N - P < P - N: r + N wraps past P, so the second comparison
		// must refuse it. A verifier that reduced r + N mod P would accept.
		x0 := big.NewInt(int64(i + 1))
		if R, ok := liftX(x0, false); ok {
			r := new(big.Int).Add(x0, new(big.Int).Sub(curveP, curveN))
			s := lowS(big.NewInt(int64(77 + i)))
			wrapX++
			check("r + N >= P", recoverKey(R, r, s, e), msg, encode(r, s), false)
		}

		// u1 G == u2 Q: the sum is a doubling. R = tG, s = 2e/t, Q = (e/r)G.
		tt := big.NewInt(int64(5 + 3*i))
		R := g.mul(tt)
		r := new(big.Int).Mod(R.x, curveN)
		s := modN(new(big.Int).Mul(new(big.Int).Lsh(e, 1), new(big.Int).ModInverse(tt, curveN)))
		if s.Cmp(new(big.Int).Rsh(curveN, 1)) > 0 {
			s.Sub(curveN, s) // -R has the same x
		}
		q := g.mul(modN(new(big.Int).Mul(e, new(big.Int).ModInverse(r, curveN))))
		w := new(big.Int).ModInverse(s, curveN)
		u1G, u2Q := g.mul(modN(new(big.Int).Mul(e, w))), q.mul(modN(new(big.Int).Mul(r, w)))
		require.True(t, u1G.equal(u2Q))
		require.Zero(t, new(big.Int).Mod(u1G.add(u2Q).x, curveN).Cmp(r))
		check("u1G == u2Q", q, msg, encode(r, s), true)

		// u1 G == -u2 Q: the sum is infinity. Q = (-e/r)G.
		r = big.NewInt(int64(9 + i))
		s = big.NewInt(int64(11 + i))
		q = g.mul(modN(new(big.Int).Neg(new(big.Int).Mul(e, new(big.Int).ModInverse(r, curveN)))))
		check("u1G == -u2Q", q, msg, encode(r, s), false)
		check("u1G == -u2Q, R + N", q, msg, encode(new(big.Int).Add(r, curveN), s), false)
	}

	require.Positive(t, highX)
	require.Positive(t, wrapX)

	// Boundary keys signed normally.
	for _, d := range []*big.Int{big.NewInt(1), big.NewInt(2), new(big.Int).Sub(curveN, big.NewInt(1)), new(big.Int).Rsh(curveN, 1)} {
		priv := &secp256k1.PrivKey{Key: d.FillBytes(make([]byte, 32))}
		sig, err := priv.Sign([]byte("edge"))
		require.NoError(t, err)
		check("boundary key", ecPointFrom(priv.PubKey().Bytes()), []byte("edge"), sig, true)
	}
}

// TestVerifyMatchesDecredKeyEncodings covers non-canonical key encodings that
// a mod-P reduction or a lax length/prefix check would accept.
func TestVerifyMatchesDecredKeyEncodings(t *testing.T) {
	priv := secp256k1.GenPrivKeyFromSecret([]byte("enc"))
	msg := []byte("enc")
	sig, err := priv.Sign(msg)
	require.NoError(t, err)
	pub := priv.PubKey().Bytes()
	unc := formats(pub)[1]
	lifted := 0
	for x := int64(1); x < 64; x++ {
		if R, ok := liftX(big.NewInt(x), false); ok {
			lifted++
			xp := new(big.Int).Add(R.x, curveP).FillBytes(make([]byte, 32))
			y := R.y.FillBytes(make([]byte, 32))
			for _, p := range [][]byte{
				append([]byte{0x02}, xp...),
				append(append([]byte{0x04}, xp...), y...),
				append(append([]byte{0x06 | y[31]&1}, xp...), y...),
			} {
				require.False(t, requireSameVerdict(t, "x + P", p, msg, sig))
			}
		}
	}
	require.Positive(t, lifted)
	for _, p := range [][]byte{
		append([]byte{0x02}, unc[1:]...), append([]byte{0x03}, unc[1:]...), append([]byte{0x05}, unc[1:]...),
		append([]byte{0x04}, pub[1:]...), append([]byte{0x06}, pub[1:]...), append([]byte{0x07}, pub[1:]...),
		pub[:32], append(append([]byte{}, pub...), 0), unc[:64], append(append([]byte{}, unc...), 0),
		append([]byte{0x02}, make([]byte, 32)...), append([]byte{0x04}, make([]byte, 64)...),
	} {
		require.False(t, requireSameVerdict(t, "encoding", p, msg, sig))
	}
	require.True(t, requireSameVerdict(t, "control", pub, msg, sig))
}

// TestVerifyNilKeyMatchesDecred: a nil key returns false on both paths for a
// bad signature length, and panics on both otherwise.
func TestVerifyNilKeyMatchesDecred(t *testing.T) {
	var pk *secp256k1.PubKey
	require.False(t, pk.VerifySignature(nil, nil))
	require.False(t, Verify(pk, nil, nil))
	require.Panics(t, func() { pk.VerifySignature(nil, make([]byte, 64)) })
	require.Panics(t, func() { Verify(pk, nil, make([]byte, 64)) })
}

type ecPoint struct{ x, y *big.Int }

func dcrG() ecPoint { c := dcrsecp.S256().Params(); return ecPoint{c.Gx, c.Gy} }

func ecPointFrom(compressed []byte) ecPoint {
	R, ok := liftX(new(big.Int).SetBytes(compressed[1:]), compressed[0] == 0x03)
	if !ok {
		panic("not on curve")
	}
	return R
}

func liftX(x *big.Int, odd bool) (ecPoint, bool) {
	if x.Cmp(curveP) >= 0 {
		return ecPoint{}, false
	}
	rhs := new(big.Int).Exp(x, big.NewInt(3), curveP)
	rhs.Add(rhs, big.NewInt(7)).Mod(rhs, curveP)
	y := new(big.Int).ModSqrt(rhs, curveP)
	if y == nil {
		return ecPoint{}, false
	}
	if y.Bit(0) == 1 != odd {
		y.Sub(curveP, y)
	}
	return ecPoint{new(big.Int).Set(x), y}, true
}

// add returns p + o; neither may be infinity and their sum must not be.
func (p ecPoint) add(o ecPoint) ecPoint {
	var l *big.Int
	if p.x.Cmp(o.x) == 0 {
		if p.y.Cmp(o.y) != 0 {
			panic("infinity")
		}
		l = new(big.Int).Mul(big.NewInt(3), new(big.Int).Mul(p.x, p.x))
		l.Mul(l, new(big.Int).ModInverse(new(big.Int).Lsh(p.y, 1), curveP))
	} else {
		l = new(big.Int).Sub(o.y, p.y)
		l.Mul(l, new(big.Int).ModInverse(new(big.Int).Mod(new(big.Int).Sub(o.x, p.x), curveP), curveP))
	}
	l.Mod(l, curveP)
	x := new(big.Int).Mul(l, l)
	x.Sub(x, p.x).Sub(x, o.x).Mod(x, curveP)
	y := new(big.Int).Sub(p.x, x)
	y.Mul(y, l).Sub(y, p.y).Mod(y, curveP)
	return ecPoint{x, y}
}

func (p ecPoint) mul(k *big.Int) ecPoint {
	var acc *ecPoint
	for i := k.BitLen() - 1; i >= 0; i-- {
		if acc != nil {
			d := acc.add(*acc)
			acc = &d
		}
		if k.Bit(i) == 1 {
			if acc == nil {
				c := p
				acc = &c
			} else {
				s := acc.add(p)
				acc = &s
			}
		}
	}
	return *acc
}

func (p ecPoint) neg() ecPoint { return ecPoint{p.x, new(big.Int).Sub(curveP, p.y)} }

func (p ecPoint) equal(o ecPoint) bool { return p.x.Cmp(o.x) == 0 && p.y.Cmp(o.y) == 0 }

func (p ecPoint) compressed() []byte {
	return append([]byte{0x02 | byte(p.y.Bit(0))}, p.x.FillBytes(make([]byte, 32))...)
}

// recoverKey returns Q = r^-1 (s R - e G), so that u1 G + u2 Q = R.
func recoverKey(rPoint ecPoint, r, s, e *big.Int) ecPoint {
	g := dcrG()
	sum := rPoint.mul(s)
	if e.Sign() != 0 {
		sum = sum.add(g.mul(e).neg())
	}
	return sum.mul(new(big.Int).ModInverse(r, curveN))
}

func msgScalar(msg []byte) *big.Int {
	h := sha256.Sum256(msg)
	return modN(new(big.Int).SetBytes(h[:]))
}

func modN(v *big.Int) *big.Int { return v.Mod(v, curveN) }

func lowS(s *big.Int) *big.Int {
	if s.Cmp(new(big.Int).Rsh(curveN, 1)) > 0 {
		return new(big.Int).Sub(curveN, s)
	}
	return s
}
