package merkle

import "crypto/sha256"

// Prefix bytes distinguish leaf from internal nodes, matching CometBFT/Tendermint.
var (
	leafPrefix  = []byte{0}
	innerPrefix = []byte{1}
)

// hashLeaf writes sha256(leafPrefix || data) into dst[:NodeSize].
func hashLeaf(data, dst []byte) {
	h := sha256.New()
	h.Write(leafPrefix)
	h.Write(data)
	h.Sum(dst[:0])
}

// hashPair writes sha256(innerPrefix || left || right) into dst[:NodeSize].
func hashPair(left, right, dst []byte) {
	h := sha256.New()
	h.Write(innerPrefix)
	h.Write(left)
	h.Write(right)
	h.Sum(dst[:0])
}
