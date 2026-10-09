// Package ed25519batch verifies ZIP-215 batches with repeated public keys.
package ed25519batch

import (
	"crypto/sha512"
	"encoding/binary"
	"errors"
	"io"
	"math/bits"

	"github.com/cometbft/cometbft/crypto"
	cmted25519 "github.com/cometbft/cometbft/crypto/ed25519"
	"github.com/oasisprotocol/curve25519-voi/curve"
	"github.com/oasisprotocol/curve25519-voi/curve/scalar"
)

type entry struct {
	r            curve.EdwardsPoint
	s, challenge scalar.Scalar
	key          int
}

// Verifier combines coefficients for identical encoded public keys. The
// cofactored equation and point/scalar decoding match CometBFT's ZIP-215 path.
type Verifier struct {
	entries        []entry
	keys           map[[32]byte]int
	publicPoints   []curve.EdwardsPoint
	invalid        bool
	scalarValues   []scalar.Scalar
	scalarPointers []*scalar.Scalar
	points         []*curve.EdwardsPoint
	random         []byte
	sums           []scalarSum
}

// Reset discards the batch while retaining bounded worker storage.
func (v *Verifier) Reset() {
	v.entries = v.entries[:0]
	// Retain a small validator set across worker batches. Unused points
	// have zero coefficients; larger sets are discarded at the next reset.
	if len(v.keys) > 128 {
		v.publicPoints = v.publicPoints[:0]
		clear(v.keys)
	}
	v.invalid = false
}

// Add queues a signature with the same key-type and size checks as CometBFT.
func (v *Verifier) Add(key crypto.PubKey, message, signature []byte) error {
	publicKey, ok := key.(cmted25519.PubKey)
	if !ok || len(publicKey) != 32 || len(signature) != 64 {
		v.invalid = true
		return errors.New("invalid Ed25519 key or signature size")
	}
	if v.invalid {
		return nil
	}
	var e entry
	if _, err := e.s.SetCanonicalBytes(signature[32:]); err != nil {
		v.invalid = true
		return nil
	}
	var compressed curve.CompressedEdwardsY
	copy(compressed[:], signature[:32])
	if _, err := e.r.SetCompressedY(&compressed); err != nil {
		v.invalid = true
		return nil
	}
	encoded := [32]byte(publicKey)
	index, exists := v.keys[encoded]
	if !exists {
		var point curve.EdwardsPoint
		copy(compressed[:], publicKey)
		if _, err := point.SetCompressedY(&compressed); err != nil {
			v.invalid = true
			return nil
		}
		if v.keys == nil {
			v.keys = make(map[[32]byte]int)
		}
		index = len(v.publicPoints)
		v.keys[encoded] = index
		v.publicPoints = append(v.publicPoints, point)
	}
	e.key = index
	h := sha512.New()
	h.Write(signature[:32])
	h.Write(publicKey)
	h.Write(message)
	var digest [sha512.Size]byte
	h.Sum(digest[:0])
	if _, err := e.challenge.SetBytesModOrderWide(digest[:]); err != nil {
		v.invalid = true
		return nil
	}
	v.entries = append(v.entries, e)
	return nil
}

// VerifyBatchOnly checks [-sum(z*s)]B + sum([z]R) + sum([z*k]A)
// after multiplication by the cofactor. Each z is independently sampled from
// {1,...,2^128}; coefficients for each repeated A are summed modulo the order.
// Failed batches remain uncached for the authoritative sequential verifier.
func (v *Verifier) VerifyBatchOnly(random io.Reader) bool {
	n := len(v.entries)
	if v.invalid || n == 0 {
		return false
	}
	terms := 1 + n + len(v.publicPoints)
	if cap(v.scalarValues) < terms {
		v.scalarValues = make([]scalar.Scalar, terms)
	}
	v.scalarValues = v.scalarValues[:terms]
	if cap(v.scalarPointers) < terms {
		v.scalarPointers = make([]*scalar.Scalar, terms)
		v.points = make([]*curve.EdwardsPoint, terms)
	}
	v.scalarPointers = v.scalarPointers[:terms]
	v.points = v.points[:terms]
	for i := range v.scalarPointers {
		v.scalarPointers[i] = &v.scalarValues[i]
	}
	v.points[0] = curve.ED25519_BASEPOINT_POINT
	for i := range v.publicPoints {
		v.points[1+n+i] = &v.publicPoints[i]
	}
	if cap(v.random) < 16*n {
		v.random = make([]byte, 16*n)
	}
	v.random = v.random[:16*n]
	if _, err := io.ReadFull(random, v.random); err != nil {
		return false
	}
	if cap(v.sums) < 1+len(v.publicPoints) {
		v.sums = make([]scalarSum, 1+len(v.publicPoints))
	}
	v.sums = v.sums[:1+len(v.publicPoints)]
	clear(v.sums)
	for i := range v.entries {
		e := &v.entries[i]
		var raw [32]byte
		copy(raw[:16], v.random[16*i:16*(i+1)])
		if raw == [32]byte{} {
			raw[16] = 1
		}
		z := &v.scalarValues[1+i]
		if _, err := z.SetBits(raw[:]); err != nil {
			return false
		}
		v.points[1+i] = &e.r
		v.sums[0].addProduct(&raw, &e.s)
		v.sums[1+e.key].addProduct(&raw, &e.challenge)
	}
	v.sums[0].reduce(&v.scalarValues[0])
	for i := range v.publicPoints {
		v.sums[1+i].reduce(&v.scalarValues[1+n+i])
	}
	v.scalarValues[0].Neg(&v.scalarValues[0])
	var result curve.EdwardsPoint
	result.MultiscalarMulVartime(v.scalarPointers, v.points)
	return result.IsSmallOrder()
}

// scalarSum accumulates unreduced products of a coefficient at most 2^128
// and a scalar below 2^253. Seven limbs hold MaxInt such products.
type scalarSum [7]uint64

func (s *scalarSum) addProduct(coefficient *[32]byte, value *scalar.Scalar) {
	var encoded [32]byte
	_ = value.ToBytes(encoded[:])
	for i := range 3 {
		word := binary.LittleEndian.Uint64(coefficient[8*i:])
		if word == 0 {
			continue
		}
		var carry uint64
		for j := range 4 {
			hi, lo := bits.Mul64(word, binary.LittleEndian.Uint64(encoded[8*j:]))
			var c uint64
			lo, c = bits.Add64(lo, s[i+j], 0)
			hi += c
			lo, c = bits.Add64(lo, carry, 0)
			hi += c
			s[i+j] = lo
			carry = hi
		}
		// Bounded rather than trusting the headroom: a future change to the
		// coefficient width or the batch size would otherwise turn this into
		// an index-out-of-range inside a worker, which the worker's recover
		// hides as a cache that never warms.
		for j := i + 4; carry != 0; j++ {
			if j >= len(s) {
				panic("ed25519batch: scalar accumulator overflowed; batch size and coefficient width are out of step")
			}
			s[j], carry = bits.Add64(s[j], carry, 0)
		}
	}
}

func (s *scalarSum) reduce(out *scalar.Scalar) {
	var encoded [64]byte
	for i, limb := range s {
		binary.LittleEndian.PutUint64(encoded[8*i:], limb)
	}
	_, _ = out.SetBytesModOrderWide(encoded[:])
}
