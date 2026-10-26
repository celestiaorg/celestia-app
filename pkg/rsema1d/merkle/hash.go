package merkle

import (
	"crypto/sha256"

	"github.com/celestiaorg/celestia-app/v11/pkg/rsema1d/internal/sha256x2"
)

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

// hashLeafPair writes hashLeaf(a) into da and hashLeaf(b) into db. On arm64 the
// two hashes run interleaved, which is faster than one after the other (see
// [sha256x2.Sum]).
func hashLeafPair(a, b, da, db []byte) {
	sha256x2.Sum(leafPrefix[0], a, b, da, db)
}

// hashPair writes sha256(innerPrefix || left || right) into dst[:NodeSize].
func hashPair(left, right, dst []byte) {
	h := sha256.New()
	h.Write(innerPrefix)
	h.Write(left)
	h.Write(right)
	h.Sum(dst[:0])
}
