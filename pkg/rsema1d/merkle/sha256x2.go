package merkle

import "encoding/binary"

var sha256IV = [8]uint32{0x6a09e667, 0xbb67ae85, 0x3c6ef372, 0xa54ff53a, 0x510e527f, 0x9b05688c, 0x1f83d9ab, 0x5be0cd19}

// hashLeaf2 writes hashLeaf(a) into da and hashLeaf(b) into db. On arm64 with
// SHA-2 instructions and equal-length inputs it hashes both at once with
// [blockSHA2x2]; otherwise it falls back to two hashLeaf calls.
func hashLeaf2(a, b, da, db []byte) {
	if !hasSHA2x2 || len(a) != len(b) || len(a) < 63 {
		hashLeaf(a, da)
		hashLeaf(b, db)
		return
	}
	sa, sb := sha256IV, sha256IV
	var ba, bb [128]byte

	// first block: prefix byte followed by 63 message bytes
	ba[0], bb[0] = leafPrefix[0], leafPrefix[0]
	copy(ba[1:64], a[:63])
	copy(bb[1:64], b[:63])
	blockSHA2x2(&sa, &sb, ba[:64], bb[:64])

	ra, rb := a[63:], b[63:]
	full := len(ra) &^ 63
	blockSHA2x2(&sa, &sb, ra[:full], rb[:full])
	ra, rb = ra[full:], rb[full:]

	// tail with padding: 0x80, zeros, then the 64-bit message bit length
	ba, bb = [128]byte{}, [128]byte{}
	copy(ba[:], ra)
	copy(bb[:], rb)
	ba[len(ra)], bb[len(rb)] = 0x80, 0x80
	n := 64
	if len(ra)+9 > 64 {
		n = 128
	}
	bits := uint64(1+len(a)) * 8
	binary.BigEndian.PutUint64(ba[n-8:n], bits)
	binary.BigEndian.PutUint64(bb[n-8:n], bits)
	blockSHA2x2(&sa, &sb, ba[:n], bb[:n])

	for i := range 8 {
		binary.BigEndian.PutUint32(da[4*i:], sa[i])
		binary.BigEndian.PutUint32(db[4*i:], sb[i])
	}
}

// hashLeafPairs writes hashLeaf(leaves[i]) into node(i) for every i in [0,n),
// two leaves at a time so [hashLeaf2] can overlap them.
func hashLeafPairs(n, workers int, leaves [][]byte, node func(i int) []byte) {
	parallelize(n/2, workers, func(j int) {
		i := 2 * j
		hashLeaf2(leaves[i], leaves[i+1], node(i), node(i+1))
	})
	if n%2 == 1 {
		hashLeaf(leaves[n-1], node(n-1))
	}
}
