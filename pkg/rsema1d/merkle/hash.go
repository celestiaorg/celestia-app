package merkle

import (
	"lukechampine.com/blake3"
	"lukechampine.com/blake3/guts"
)

// Prefix bytes distinguish leaf from internal nodes, matching CometBFT/Tendermint.
const (
	leafPrefix  = byte(0)
	innerPrefix = byte(1)
)

// innerNodeSize is prefix(1) + left(32) + right(32) = 65 bytes.
const innerNodeSize = 1 + 2*NodeSize

// smallLeafThreshold is the max leaf data length for which we compose
// prefix||data on the stack and hash via the single-shot guts path — avoiding
// Hasher init. Sized to cover RLC-tree leaves (16 B) and all inner nodes
// (65 B); larger leaves fall back through blakeRoot.
const smallLeafThreshold = 256

// hashLeaf writes blake3(leafPrefix || data) into dst[:NodeSize].
//
// Always composes prefix||data into a stack/worker-owned scratch and dispatches
// to the alloc-free, goroutine-free guts path. blake3.Hasher.Write is avoided
// because for multi-chunk inputs it heap-allocates and spawns goroutines — both
// pathological when this call already runs inside our outer worker pool.
func hashLeaf(data, dst []byte) {
	if len(data) < smallLeafThreshold {
		var buf [1 + smallLeafThreshold]byte
		buf[0] = leafPrefix
		n := copy(buf[1:], data)
		r := blakeRootSmall(buf[:1+n])
		copy(dst, r[:])
		return
	}
	scratch := make([]byte, 1+len(data))
	scratch[0] = leafPrefix
	copy(scratch[1:], data)
	r := blakeRoot(scratch)
	copy(dst, r[:])
}

// hashPair writes blake3(innerPrefix || left || right) into dst[:NodeSize].
// 65 B fits BLAKE3's single-chunk fast path; we compose on the stack to avoid
// Hasher allocation.
func hashPair(left, right, dst []byte) {
	var buf [innerNodeSize]byte
	buf[0] = innerPrefix
	copy(buf[1:1+NodeSize], left)
	copy(buf[1+NodeSize:], right)
	r := blakeRootSmall(buf[:])
	copy(dst, r[:])
}

// blakeRootSmall computes BLAKE3(b) → 32 bytes with no allocations and no
// internal goroutines. Requires len(b) <= MaxSIMD*ChunkSize (16 KiB). Splitting
// from blakeRootLarge keeps the streaming Hasher's heap-escape pessimism out of
// the fast path.
func blakeRootSmall(b []byte) [NodeSize]byte {
	switch {
	case len(b) <= guts.BlockSize:
		var block [64]byte
		copy(block[:], b)
		return finalize(guts.Node{
			CV:       guts.IV,
			Block:    guts.BytesToWords(block),
			BlockLen: uint32(len(b)),
			Flags:    guts.FlagChunkStart | guts.FlagChunkEnd | guts.FlagRoot,
		})
	case len(b) <= guts.ChunkSize:
		n := guts.CompressChunk(b, &guts.IV, 0, 0)
		n.Flags |= guts.FlagRoot
		return finalize(n)
	default:
		// len(b) <= MaxSIMD*ChunkSize is the caller's invariant.
		var buf [guts.MaxSIMD * guts.ChunkSize]byte
		copy(buf[:], b)
		n := guts.CompressBuffer(&buf, len(b), &guts.IV, 0, 0)
		n.Flags |= guts.FlagRoot
		return finalize(n)
	}
}

// blakeRootLarge handles inputs > 16 KiB via the streaming Hasher. Allocates
// internally; reserved for the rare leaf size that exceeds MaxSIMD chunks.
func blakeRootLarge(b []byte) (out [NodeSize]byte) {
	h := blake3.New(NodeSize, nil)
	h.Write(b)
	h.Sum(out[:0])
	return
}

// blakeRoot dispatches on size.
func blakeRoot(b []byte) [NodeSize]byte {
	if len(b) <= guts.MaxSIMD*guts.ChunkSize {
		return blakeRootSmall(b)
	}
	return blakeRootLarge(b)
}

// finalize compresses a root Node and returns its first NodeSize bytes.
func finalize(n guts.Node) (out [NodeSize]byte) {
	words := guts.WordsToBytes(guts.CompressNode(n))
	copy(out[:], words[:NodeSize])
	return
}
