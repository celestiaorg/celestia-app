//go:build cgo

package secpverify

import (
	"crypto/sha256"
	"encoding/binary"
	"math/big"
	"math/rand/v2"
	"testing"

	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	dcrsecp "github.com/decred/dcrd/dcrec/secp256k1/v4"
	gethsecp "github.com/ethereum/go-ethereum/crypto/secp256k1"
	"github.com/stretchr/testify/require"
)

var (
	curveN = dcrsecp.S256().N
	curveP = dcrsecp.S256().P
)

// vector is one verification input; accept, when set, is decred's expected verdict.
type vector struct {
	name          string
	pub, msg, sig []byte
	accept        *bool
	// overflow marks R or S encoded as >= N, which raw libsecp256k1 rejects.
	overflow bool
}

func ptr(b bool) *bool { return &b }

// requireSameVerdict checks Verify against the SDK's decred verifier, which
// CGO=0 nodes run.
func requireSameVerdict(t testing.TB, name string, pub, msg, sig []byte) bool {
	t.Helper()
	pk := &secp256k1.PubKey{Key: pub}
	want := pk.VerifySignature(msg, sig)
	require.Equal(t, want, Verify(pk, msg, sig), "%s: pub=%x msg=%x sig=%x", name, pub, msg, sig)
	require.Equal(t, want, PubKey(pk).VerifySignature(msg, sig), name)
	return want
}

func TestVerifyMatchesDecred(t *testing.T) {
	vectors := testVectors(t, rand.New(rand.NewChaCha8([32]byte{1})))
	accepted, overflows := 0, 0
	for _, v := range vectors {
		got := requireSameVerdict(t, v.name, v.pub, v.msg, v.sig)
		if v.accept != nil {
			require.Equal(t, *v.accept, got, v.name)
		}
		if got {
			accepted++
		}
		if v.overflow {
			overflows++
			h := sha256.Sum256(v.msg)
			require.False(t, gethsecp.VerifySignature(v.pub, h[:], v.sig), "%s: raw libsecp256k1 must reject R or S >= N", v.name)
		}
	}
	require.Greater(t, overflows, 0)
	t.Logf("%d vectors, %d accepted, %d with R or S >= N", len(vectors), accepted, overflows)
}

func FuzzVerifyMatchesDecred(f *testing.F) {
	for _, v := range testVectors(f, rand.New(rand.NewChaCha8([32]byte{2}))) {
		f.Add(v.pub, v.msg, v.sig)
	}
	f.Fuzz(func(t *testing.T, pub, msg, sig []byte) {
		requireSameVerdict(t, "fuzz", pub, msg, sig)
	})
}

// FuzzVerifyMatchesDecredSigned starts from valid signatures so mutations reach
// the accept path and its edges.
func FuzzVerifyMatchesDecredSigned(f *testing.F) {
	for i := range 16 {
		f.Add([]byte{byte(i)}, []byte("msg"), uint16(i*37), uint8(i))
	}
	f.Fuzz(func(t *testing.T, seed, msg []byte, bit uint16, mode uint8) {
		priv := secp256k1.GenPrivKeyFromSecret(seed)
		pub := priv.PubKey().Bytes()
		sig, err := priv.Sign(msg)
		require.NoError(t, err)
		switch mode % 6 {
		case 1:
			sig[bit/8%64] ^= 1 << (bit % 8)
		case 2:
			pub[bit/8%33] ^= 1 << (bit % 8)
		case 3:
			sig = highS(sig)
		case 4, 5:
			// A key recovered for a small R or S, encoded as R + N or S + N.
			small := new(big.Int).SetUint64(uint64(bit) + 1)
			r, s := new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:])
			if mode%6 == 4 {
				r = small
			} else {
				s = small
			}
			k, ok := recoveredKey(r, s, msg)
			if !ok {
				return
			}
			pub, sig = k, encode(r, s)
			if mode%6 == 4 {
				sig = encode(new(big.Int).Add(r, curveN), s)
			} else {
				sig = encode(r, new(big.Int).Add(s, curveN))
			}
		}
		for _, p := range formats(pub) {
			requireSameVerdict(t, "signed", p, msg, sig)
		}
	})
}

func testVectors(t testing.TB, rng *rand.Rand) []vector {
	t.Helper()
	var vs []vector
	add := func(name string, pub, msg, sig []byte, accept *bool) {
		vs = append(vs, vector{name: name, pub: pub, msg: msg, sig: sig, accept: accept})
	}
	halfN := new(big.Int).Rsh(curveN, 1)
	for i := range 64 {
		priv := secp256k1.GenPrivKeyFromSecret(binary.BigEndian.AppendUint64(nil, rng.Uint64()))
		pub := priv.PubKey().Bytes()
		msg := make([]byte, i)
		for j := range msg {
			msg[j] = byte(rng.Uint32())
		}
		sig, err := priv.Sign(msg)
		require.NoError(t, err)
		r, s := new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:])

		for j, p := range formats(pub) {
			add("valid", p, msg, sig, ptr(j != 3))
		}
		add("wrong msg", pub, append(append([]byte{}, msg...), 0), sig, ptr(false))
		add("high S", pub, msg, highS(sig), ptr(false))
		add("R = 0", pub, msg, encode(big.NewInt(0), s), ptr(false))
		add("S = 0", pub, msg, encode(r, big.NewInt(0)), ptr(false))
		add("R = N", pub, msg, encode(curveN, s), ptr(false))
		add("S = N", pub, msg, encode(r, curveN), ptr(false))
		add("swapped", pub, msg, encode(s, r), nil)
		for range 8 {
			flipped := append([]byte{}, sig...)
			flipped[rng.IntN(64)] ^= 1 << rng.IntN(8)
			add("sig bit flip", pub, msg, flipped, nil)
			fp := append([]byte{}, pub...)
			fp[rng.IntN(33)] ^= 1 << rng.IntN(8)
			add("pub bit flip", fp, msg, sig, nil)
		}
		for _, n := range []int{0, 63, 65} {
			add("sig length", pub, msg, append(append([]byte{}, sig...), make([]byte, 2)...)[:n], ptr(false))
		}

		// Keys recovered for chosen (R, S): small R or S encoded with N added,
		// and S on either side of N/2.
		small := big.NewInt(int64(i + 1))
		for _, c := range []struct {
			name       string
			r, s       *big.Int
			rAdd, sAdd bool
			accept     bool
		}{
			{"recovered small R", small, s, false, false, true},
			{"recovered R + N", small, s, true, false, true},
			{"recovered S + N", r, small, false, true, true},
			{"recovered R + N, S + N", small, small, true, true, true},
			{"recovered S = N/2", r, halfN, false, false, true},
			{"recovered S = N/2 + 1", r, new(big.Int).Add(halfN, big.NewInt(1)), false, false, false},
		} {
			k, ok := recoveredKey(c.r, c.s, msg)
			if !ok {
				continue
			}
			er, es := c.r, c.s
			if c.rAdd {
				er = new(big.Int).Add(er, curveN)
			}
			if c.sAdd {
				es = new(big.Int).Add(es, curveN)
			}
			for j, p := range formats(k) {
				vs = append(vs, vector{name: c.name, pub: p, msg: msg, sig: encode(er, es), accept: ptr(c.accept && j != 3), overflow: c.rAdd || c.sAdd})
			}
			vs = append(vs, vector{name: c.name + ", wrong msg", pub: k, msg: append([]byte{1}, msg...), sig: encode(er, es), accept: ptr(false), overflow: c.rAdd || c.sAdd})
		}
	}

	valid := vs[0]
	msg, sig := valid.msg, valid.sig
	add("zero sig", valid.pub, msg, make([]byte, 64), ptr(false))
	add("0xff sig", valid.pub, msg, fill(64, 0xff), ptr(false))
	add("nil sig", valid.pub, msg, nil, ptr(false))
	add("nil pub", nil, msg, sig, ptr(false))
	add("zero pub", make([]byte, 33), msg, sig, ptr(false))
	add("short pub", valid.pub[:32], msg, sig, ptr(false))
	for _, prefix := range []byte{0x00, 0x01, 0x04, 0x05, 0x06, 0x07, 0xff} {
		add("pub prefix", append([]byte{prefix}, valid.pub[1:]...), msg, sig, ptr(false))
	}
	for _, x := range []*big.Int{curveP, new(big.Int).Add(curveP, big.NewInt(1)), new(big.Int).Lsh(big.NewInt(1), 256-1)} {
		xb := x.FillBytes(make([]byte, 32))
		add("pub x >= P", append([]byte{0x02}, xb...), msg, sig, ptr(false))
	}
	for x := int64(1); x < 32; x++ {
		add("pub small x", append([]byte{0x02}, big.NewInt(x).FillBytes(make([]byte, 32))...), msg, sig, nil)
	}
	unc := formats(valid.pub)[1]
	notOnCurve := append([]byte{}, unc...)
	notOnCurve[64] ^= 1
	add("pub not on curve", notOnCurve, msg, sig, ptr(false))
	add("pub y >= P", append(append([]byte{0x04}, unc[1:33]...), curveP.FillBytes(make([]byte, 32))...), msg, sig, ptr(false))
	for range 512 {
		pub, sig := make([]byte, []int{33, 65, rng.IntN(70)}[rng.IntN(3)]), make([]byte, 64)
		for i := range pub {
			pub[i] = byte(rng.Uint32())
		}
		for i := range sig {
			sig[i] = byte(rng.Uint32())
		}
		if len(pub) > 0 {
			pub[0] = []byte{2, 3, 4, 6, 7}[rng.IntN(5)]
		}
		add("random", pub, msg, sig, nil)
	}
	return vs
}

// formats returns pub compressed, uncompressed, hybrid, and hybrid with the
// wrong parity byte.
func formats(compressed []byte) [][]byte {
	k, err := dcrsecp.ParsePubKey(compressed)
	if err != nil {
		return [][]byte{compressed}
	}
	unc := k.SerializeUncompressed()
	hybrid := append([]byte{}, unc...)
	hybrid[0] = 0x06 | unc[64]&1
	wrong := append([]byte{}, hybrid...)
	wrong[0] ^= 1
	return [][]byte{compressed, unc, hybrid, wrong}
}

// recoveredKey returns the compressed key Q = r^-1 (s R - z G) for which (r, s)
// is a valid signature of msg, where R is the point with x = r.
func recoveredKey(r, s *big.Int, msg []byte) ([]byte, bool) {
	var x, y dcrsecp.FieldVal
	if x.SetByteSlice(r.FillBytes(make([]byte, 32))) || !dcrsecp.DecompressY(&x, false, &y) {
		return nil, false
	}
	var rs, ss, z dcrsecp.ModNScalar
	if rs.SetByteSlice(r.Bytes()) || ss.SetByteSlice(s.Bytes()) || rs.IsZero() {
		return nil, false
	}
	h := sha256.Sum256(msg)
	z.SetByteSlice(h[:])
	z.Negate()
	R := dcrsecp.MakeJacobianPoint(&x, &y, new(dcrsecp.FieldVal).SetInt(1))
	var sR, zG, sum, q dcrsecp.JacobianPoint
	dcrsecp.ScalarMultNonConst(&ss, &R, &sR)
	dcrsecp.ScalarBaseMultNonConst(&z, &zG)
	dcrsecp.AddNonConst(&sR, &zG, &sum)
	dcrsecp.ScalarMultNonConst(new(dcrsecp.ModNScalar).InverseValNonConst(&rs), &sum, &q)
	if (q.X.IsZero() && q.Y.IsZero()) || q.Z.IsZero() {
		return nil, false
	}
	q.ToAffine()
	return dcrsecp.NewPublicKey(&q.X, &q.Y).SerializeCompressed(), true
}

func highS(sig []byte) []byte {
	s := new(big.Int).SetBytes(sig[32:])
	return encode(new(big.Int).SetBytes(sig[:32]), s.Sub(curveN, s))
}

func encode(r, s *big.Int) []byte {
	return append(r.FillBytes(make([]byte, 32)), s.FillBytes(make([]byte, 32))...)
}

func fill(n int, b byte) []byte {
	out := make([]byte, n)
	for i := range out {
		out[i] = b
	}
	return out
}
