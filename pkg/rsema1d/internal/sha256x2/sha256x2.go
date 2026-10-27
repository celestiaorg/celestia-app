// Package sha256x2 computes two SHA-256 digests at once.
//
// On Neoverse-V2 a single SHA-256 stream is latency-bound: the SHA2 unit
// stalls between dependent rounds. On arm64 with SHA2 instructions, [Sum]
// interleaves two copies of crypto/sha256's arm64 block function so the two
// messages' dependent chains overlap. Everywhere else, and for inputs of
// unequal length, it computes the digests one after the other with
// crypto/sha256. The digests are identical either way.
package sha256x2

import (
	"crypto/sha256"
	"encoding/binary"
)

// Sum writes sha256(prefix || a) into da and sha256(prefix || b) into db.
// da and db must hold at least [sha256.Size] bytes.
func Sum(prefix byte, a, b, da, db []byte) {
	// The prefix and the first 63 message bytes form the first block, so
	// shorter inputs, which fit in one or two blocks, gain nothing.
	if !enabled || len(a) != len(b) || len(a) < 63 {
		sum(prefix, a, da)
		sum(prefix, b, db)
		return
	}
	sa, sb := iv, iv
	var ba, bb [2 * blockSize]byte

	// first block: prefix followed by 63 message bytes
	ba[0], bb[0] = prefix, prefix
	copy(ba[1:blockSize], a[:63])
	copy(bb[1:blockSize], b[:63])
	block(&sa, &sb, ba[:blockSize], bb[:blockSize])

	// full blocks straight from the inputs
	ra, rb := a[63:], b[63:]
	full := len(ra) &^ (blockSize - 1)
	block(&sa, &sb, ra[:full], rb[:full])
	ra, rb = ra[full:], rb[full:]

	// tail with padding: 0x80, zeros, then the message length in bits
	ba, bb = [2 * blockSize]byte{}, [2 * blockSize]byte{}
	copy(ba[:], ra)
	copy(bb[:], rb)
	ba[len(ra)], bb[len(rb)] = 0x80, 0x80
	n := blockSize
	if len(ra)+9 > blockSize {
		n = 2 * blockSize
	}
	bits := uint64(1+len(a)) * 8
	binary.BigEndian.PutUint64(ba[n-8:n], bits)
	binary.BigEndian.PutUint64(bb[n-8:n], bits)
	block(&sa, &sb, ba[:n], bb[:n])

	for i := range 8 {
		binary.BigEndian.PutUint32(da[4*i:], sa[i])
		binary.BigEndian.PutUint32(db[4*i:], sb[i])
	}
}

// sum writes sha256(prefix || data) into dst.
func sum(prefix byte, data, dst []byte) {
	h := sha256.New()
	h.Write(prefixes[prefix : int(prefix)+1])
	h.Write(data)
	h.Sum(dst[:0])
}

// prefixes holds every byte value, so [sum] can write its prefix from a
// static slice instead of allocating one per call.
var prefixes = func() (p [256]byte) {
	for i := range p {
		p[i] = byte(i)
	}
	return p
}()

const blockSize = sha256.BlockSize

// iv is the SHA-256 initial hash value.
var iv = [8]uint32{0x6a09e667, 0xbb67ae85, 0x3c6ef372, 0xa54ff53a, 0x510e527f, 0x9b05688c, 0x1f83d9ab, 0x5be0cd19}
