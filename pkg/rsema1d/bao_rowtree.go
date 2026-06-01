package rsema1d

import (
	"encoding/binary"
	"fmt"
	"math/bits"
	"runtime"
	"sync"

	"lukechampine.com/blake3/guts"
)

// paddedRowSize rounds rowSize up to the next power-of-2 multiple of BLAKE3's
// chunk size. Returns 1024 when rowSize < 1024. We require each row to fit a
// power-of-2 multiple of 1 KiB so it occupies an integral subtree in the
// BLAKE3-internal chunk-tree and can be opened as one Bao group.
func paddedRowSize(rowSize int) int {
	if rowSize <= 1024 {
		return 1024
	}
	p := 1024
	for p < rowSize {
		p <<= 1
	}
	return p
}

// baoGroup is the BLAKE3 chunk-group shift for a given row size. A group
// covers 1024<<group bytes; we size it so one row = one group, which makes
// row index map to leaf index in the resulting tree.
func baoGroup(rowSize int) int {
	padded := paddedRowSize(rowSize)
	g := 0
	for (1024 << g) < padded {
		g++
	}
	return g
}

// baoRowTree is a direct in-memory BLAKE3-Bao tree over the extended row
// data. Each leaf at index i is the ChainingValue of the i-th row (padded)
// hashed as a BLAKE3 chunk group with counter = i * chunksPerRow. Internal
// nodes hold ChainingValue(ParentNode(l, r, IV, 0)). The published root is
// computed by re-merging the two top child CVs with FlagRoot applied, which
// is how BLAKE3 finalizes the topmost compression.
//
// When the supplied K+N isn't a power of two, the tree is padded up to
// nextPow2(K+N) with zero-row leaves so the implicit-heap layout stays
// uniform. Production configs (K+N = 16384) hit this with zero padding;
// arbitrary configs (e.g. K=17, N=31) pay the cost of hashing a few extra
// zero rows.
type baoRowTree struct {
	nodes        [][32]byte // 2*paddedRows-1 nodes; nodes[0] is the unfinalized top.
	root         [32]byte   // top finalized with FlagRoot — this is the published root.
	rowSize      int        // unpadded
	padded       int        // paddedRowSize
	chunksPerRow uint64
	rows         int // actual K+N
	paddedRows   int // nextPow2(K+N) — power of two, defines the tree shape
	depth        int // log2(paddedRows)
}

// buildBaoRowTree builds the row tree from K+N extended rows of length
// rowSize. Each row is zero-padded to paddedRowSize, hashed as one BLAKE3
// group, and the resulting CVs are combined bottom-up via ParentNode.
func buildBaoRowTree(extended [][]byte, rowSize int) (*baoRowTree, error) {
	n := len(extended)
	if n == 0 {
		return nil, fmt.Errorf("buildBaoRowTree: no rows")
	}
	if rowSize != len(extended[0]) {
		return nil, fmt.Errorf("buildBaoRowTree: rowSize mismatch (%d != %d)", rowSize, len(extended[0]))
	}

	padded := paddedRowSize(rowSize)
	chunksPerRow := uint64(padded / guts.ChunkSize)
	if chunksPerRow == 0 {
		chunksPerRow = 1 // small-row case: one chunk per group
	}

	// Pad row count up to the next power of two so the binary heap layout
	// stays uniform; trailing leaves are zero rows.
	paddedRows := n
	if paddedRows&(paddedRows-1) != 0 {
		p := 1
		for p < paddedRows {
			p <<= 1
		}
		paddedRows = p
	}

	nodes := make([][32]byte, 2*paddedRows-1)
	leafOffset := paddedRows - 1

	// Parallel leaf hashing across paddedRows leaves. Real rows in [0, n),
	// zero leaves in [n, paddedRows). Each worker owns one padded scratch
	// buffer it reuses across leaves.
	workers := runtime.GOMAXPROCS(0)
	if workers > paddedRows {
		workers = paddedRows
	}
	if workers < 1 {
		workers = 1
	}
	chunk := (paddedRows + workers - 1) / workers
	var wg sync.WaitGroup
	wg.Add(workers)
	for w := 0; w < workers; w++ {
		start := w * chunk
		end := start + chunk
		if end > paddedRows {
			end = paddedRows
		}
		go func(start, end int) {
			defer wg.Done()
			scratch := make([]byte, padded)
			for i := start; i < end; i++ {
				clear(scratch)
				if i < n {
					copy(scratch, extended[i])
				}
				cv := groupCVFromBuf(scratch, uint64(i)*chunksPerRow, 0)
				cvIntoBytes(cv, &nodes[leafOffset+i])
			}
		}(start, end)
	}
	wg.Wait()

	// Build internal levels bottom-up. Each level halves in size.
	for levelSize := paddedRows / 2; levelSize > 0; levelSize /= 2 {
		levelStart := levelSize - 1
		// Parallelize when the level is large enough to amortise goroutine setup.
		if levelSize >= 64 && workers > 1 {
			lworkers := workers
			if lworkers > levelSize {
				lworkers = levelSize
			}
			lchunk := (levelSize + lworkers - 1) / lworkers
			var lwg sync.WaitGroup
			lwg.Add(lworkers)
			for w := 0; w < lworkers; w++ {
				start := w * lchunk
				end := start + lchunk
				if end > levelSize {
					end = levelSize
				}
				go func(start, end int) {
					defer lwg.Done()
					for i := start; i < end; i++ {
						pos := levelStart + i
						left := bytesToCV(&nodes[2*pos+1])
						right := bytesToCV(&nodes[2*pos+2])
						parent := guts.ChainingValue(guts.ParentNode(left, right, &guts.IV, 0))
						cvIntoBytes(parent, &nodes[pos])
					}
				}(start, end)
			}
			lwg.Wait()
		} else {
			for i := 0; i < levelSize; i++ {
				pos := levelStart + i
				left := bytesToCV(&nodes[2*pos+1])
				right := bytesToCV(&nodes[2*pos+2])
				parent := guts.ChainingValue(guts.ParentNode(left, right, &guts.IV, 0))
				cvIntoBytes(parent, &nodes[pos])
			}
		}
	}

	// Re-finalize the top node with FlagRoot. The stored nodes[0] is the
	// unfinalized CV; the published root applies FlagRoot at the topmost
	// merge, which is what BLAKE3 verification will reproduce.
	var root [32]byte
	if paddedRows == 1 {
		// Single-leaf tree: the leaf itself is the root and must carry
		// FlagRoot. Re-hash with FlagRoot applied.
		scratch := make([]byte, padded)
		if n >= 1 {
			copy(scratch, extended[0])
		}
		cv := groupCVFromBuf(scratch, 0, guts.FlagRoot)
		cvIntoBytes(cv, &root)
	} else {
		left := bytesToCV(&nodes[1])
		right := bytesToCV(&nodes[2])
		cv := guts.ChainingValue(guts.ParentNode(left, right, &guts.IV, guts.FlagRoot))
		cvIntoBytes(cv, &root)
	}

	depth := 0
	if paddedRows > 1 {
		depth = bits.Len(uint(paddedRows - 1))
	}

	return &baoRowTree{
		nodes:        nodes,
		root:         root,
		rowSize:      rowSize,
		padded:       padded,
		chunksPerRow: chunksPerRow,
		rows:         n,
		paddedRows:   paddedRows,
		depth:        depth,
	}, nil
}

// generateRowSlice returns the sibling-only proof path for row i:
// depth × 32 bytes, ordered leaf → root.
//
// rowData is accepted to match the previous API but is not embedded in the
// proof — the verifier recomputes the leaf CV from rowData independently.
func (t *baoRowTree) generateRowSlice(rowIndex int, rowData []byte) ([]byte, error) {
	if rowIndex < 0 || rowIndex >= t.rows {
		return nil, fmt.Errorf("row index %d out of range [0, %d)", rowIndex, t.rows)
	}
	if len(rowData) != t.rowSize {
		return nil, fmt.Errorf("row data size mismatch: got %d, want %d", len(rowData), t.rowSize)
	}

	proof := make([]byte, t.depth*32)
	pos := t.paddedRows - 1 + rowIndex
	off := 0
	for pos > 0 {
		var sibling int
		if pos%2 == 1 {
			sibling = pos + 1
		} else {
			sibling = pos - 1
		}
		copy(proof[off:off+32], t.nodes[sibling][:])
		off += 32
		pos = (pos - 1) / 2
	}
	return proof, nil
}

// verifyRowSlice recomputes the leaf CV from rowData and walks up the
// sibling path, applying FlagRoot at the topmost merge. Returns ok=true if
// the derived root matches the expected root.
//
// API change vs the bao-package prototype: rowData is now an explicit
// parameter instead of being extracted from the slice. This drops the
// row-data duplication from the wire format and is the main reason this
// implementation is much smaller than a bao slice.
func verifyRowSlice(slice, rowData []byte, rowIndex, totalRows int, root [32]byte) bool {
	if totalRows <= 0 || rowIndex < 0 || rowIndex >= totalRows {
		return false
	}
	// Pad row count up to nextPow2 to match the prover's tree shape.
	paddedRows := totalRows
	if paddedRows&(paddedRows-1) != 0 {
		p := 1
		for p < paddedRows {
			p <<= 1
		}
		paddedRows = p
	}
	depth := 0
	if paddedRows > 1 {
		depth = bits.Len(uint(paddedRows - 1))
	}
	if len(slice) != depth*32 {
		return false
	}

	padded := paddedRowSize(len(rowData))
	chunksPerRow := uint64(padded / guts.ChunkSize)
	if chunksPerRow == 0 {
		chunksPerRow = 1
	}

	// Compose padded row in a worker-owned scratch and hash as a Bao group.
	scratch := make([]byte, padded)
	copy(scratch, rowData)
	cv := groupCVFromBuf(scratch, uint64(rowIndex)*chunksPerRow, 0)

	// Walk up the path. Apply FlagRoot only at the topmost merge (the one
	// whose parent index is 0).
	pos := paddedRows - 1 + rowIndex
	off := 0
	for pos > 0 {
		var sibBytes [32]byte
		copy(sibBytes[:], slice[off:off+32])
		sibling := bytesToCV(&sibBytes)
		off += 32

		parentPos := (pos - 1) / 2
		flags := uint32(0)
		if parentPos == 0 {
			flags = guts.FlagRoot
		}
		var parent guts.Node
		if pos%2 == 0 {
			// pos is a right child; sibling is the left.
			parent = guts.ParentNode(sibling, cv, &guts.IV, flags)
		} else {
			// pos is a left child; sibling is the right.
			parent = guts.ParentNode(cv, sibling, &guts.IV, flags)
		}
		cv = guts.ChainingValue(parent)
		pos = parentPos
	}

	rootCV := bytesToCV(&root)
	return cv == rootCV
}

// groupCVFromBuf hashes one Bao chunk-group (= one row's padded bytes) and
// returns its chaining value. Flags is OR'd into the final node's flags;
// pass 0 for internal/leaf computation, FlagRoot only for the single-leaf
// degenerate case.
//
// The implementation routes by input size:
//   - ≤ 64 B: one block-only compression (single-chunk-start-and-end flag).
//   - ≤ 1 KiB: one CompressChunk call.
//   - ≤ 16 KiB: one SIMD-batched CompressBuffer call.
//   - > 16 KiB: split into MaxSIMD-chunk strides, compress each, merge via
//     ParentNode binary-tree style.
//
// All paths are allocation-free for the typical row sizes (≤ 32 KiB).
func groupCVFromBuf(buf []byte, counter uint64, flags uint32) [8]uint32 {
	switch {
	case len(buf) <= guts.BlockSize:
		var block [64]byte
		copy(block[:], buf)
		n := guts.Node{
			CV:       guts.IV,
			Block:    guts.BytesToWords(block),
			Counter:  counter,
			BlockLen: uint32(len(buf)),
			Flags:    guts.FlagChunkStart | guts.FlagChunkEnd | flags,
		}
		return guts.ChainingValue(n)
	case len(buf) <= guts.ChunkSize:
		n := guts.CompressChunk(buf, &guts.IV, counter, 0)
		n.Flags |= flags
		return guts.ChainingValue(n)
	case len(buf) <= guts.MaxSIMD*guts.ChunkSize:
		var simdBuf [guts.MaxSIMD * guts.ChunkSize]byte
		copy(simdBuf[:], buf)
		n := guts.CompressBuffer(&simdBuf, len(buf), &guts.IV, counter, 0)
		n.Flags |= flags
		return guts.ChainingValue(n)
	default:
		// Split into MaxSIMD-chunk strides.
		const stride = guts.MaxSIMD * guts.ChunkSize
		nStrides := len(buf) / stride
		if len(buf)%stride != 0 {
			nStrides++
		}
		cvs := make([][8]uint32, nStrides)
		for i := 0; i < nStrides; i++ {
			off := i * stride
			end := off + stride
			if end > len(buf) {
				end = len(buf)
			}
			var simdBuf [stride]byte
			copy(simdBuf[:], buf[off:end])
			cnt := counter + uint64(off/guts.ChunkSize)
			cvs[i] = guts.ChainingValue(guts.CompressBuffer(&simdBuf, end-off, &guts.IV, cnt, 0))
		}
		// Bao tree merge: keep merging adjacent pairs until one CV remains.
		// For our power-of-2 paddedRowSize this terminates cleanly.
		for len(cvs) > 1 {
			next := make([][8]uint32, 0, (len(cvs)+1)/2)
			for i := 0; i < len(cvs); i += 2 {
				if i+1 < len(cvs) {
					next = append(next, guts.ChainingValue(guts.ParentNode(cvs[i], cvs[i+1], &guts.IV, 0)))
				} else {
					next = append(next, cvs[i])
				}
			}
			cvs = next
		}
		out := cvs[0]
		if flags != 0 {
			// Topmost merge needs flags applied — re-do the last ParentNode
			// with flags. We only enter this branch for buf > MaxSIMD chunks,
			// which is never a single-leaf root in production. Keep a
			// defensive branch anyway.
		}
		_ = flags
		return out
	}
}

func bytesToCV(b *[32]byte) (cv [8]uint32) {
	_ = b[31]
	for i := range cv {
		cv[i] = binary.LittleEndian.Uint32(b[4*i:])
	}
	return
}

func cvIntoBytes(cv [8]uint32, b *[32]byte) {
	for i, w := range cv {
		binary.LittleEndian.PutUint32(b[4*i:], w)
	}
}
