package merkle

import (
	"bytes"
	"crypto/sha256"
	"testing"
)

// White-box tests for the unexported hashLeaf, hashPair primitives. These are
// the building blocks of the tree; pinning their byte-level behaviour against
// SHA256 keeps the public API contract honest. Other internal-API checks
// (numLeaves, depth) also live here so the external _test package can stay
// focused on the public surface.

func TestHashPair(t *testing.T) {
	left := hashLeafTest([]byte("left data"))
	right := hashLeafTest([]byte("right data"))

	// Test that hashPair is deterministic
	hash1 := hashPairTest(left, right)
	hash2 := hashPairTest(left, right)

	if !bytes.Equal(hash1, hash2) {
		t.Error("hashPair is not deterministic")
	}

	// Test that order matters
	hash3 := hashPairTest(right, left)
	if bytes.Equal(hash1, hash3) {
		t.Error("hashPair(left, right) should differ from hashPair(right, left)")
	}

	// Test expected length
	if len(hash1) != 32 {
		t.Errorf("hashPair returned %d bytes, expected 32", len(hash1))
	}

	// Test with actual SHA256 (now includes inner prefix)
	h := sha256.New()
	h.Write(innerPrefix)
	h.Write(left)
	h.Write(right)
	expected := h.Sum(nil)

	if !bytes.Equal(hash1, expected) {
		t.Error("hashPair does not match expected SHA256 output")
	}
}

func TestHashLeaf(t *testing.T) {
	data := []byte("leaf data")

	// Test that hashLeaf is deterministic
	hash1 := hashLeafTest(data)
	hash2 := hashLeafTest(data)

	if !bytes.Equal(hash1, hash2) {
		t.Error("hashLeaf is not deterministic")
	}

	// Test expected length
	if len(hash1) != 32 {
		t.Errorf("hashLeaf returned %d bytes, expected 32", len(hash1))
	}

	// Test with actual SHA256 (includes leaf prefix)
	h := sha256.New()
	h.Write(leafPrefix)
	h.Write(data)
	expected := h.Sum(nil)

	if !bytes.Equal(hash1, expected) {
		t.Error("hashLeaf does not match expected SHA256 output")
	}

	// Test that hashLeaf differs from raw hash
	h2 := sha256.New()
	h2.Write(data)
	rawHash := h2.Sum(nil)

	if bytes.Equal(hash1, rawHash) {
		t.Error("hashLeaf should differ from raw SHA256 due to leaf prefix")
	}
}

// TestTreeNumLeaves exercises the unexported numLeaves accessor across the
// shapes NewTree is built for.
func TestTreeNumLeaves(t *testing.T) {
	for _, n := range []int{1, 2, 4, 8, 64} {
		leaves := makeTestLeavesInternal(n)
		tree := NewTree(leaves, 1)
		if got := tree.numLeaves(); got != n {
			t.Errorf("numLeaves() = %d, want %d", got, n)
		}
	}
}

// TestTreeDepthInternal verifies the unexported depth() accessor matches the
// log2 of the leaf count across the supported tree shapes.
func TestTreeDepthInternal(t *testing.T) {
	tests := []struct {
		numLeaves int
		wantDepth int
	}{
		{1, 0},
		{2, 1},
		{4, 2},
		{8, 3},
		{16, 4},
		{32, 5},
		{64, 6},
		{128, 7},
		{256, 8},
	}
	for _, tt := range tests {
		leaves := makeTestLeavesInternal(tt.numLeaves)
		tree := NewTree(leaves, 1)
		if depth := tree.depth(); depth != tt.wantDepth {
			t.Errorf("depth() with %d leaves = %d, want %d", tt.numLeaves, depth, tt.wantDepth)
		}
	}
}

// Helper functions for the internal-package tests.

func hashLeafTest(data []byte) []byte {
	var result [32]byte
	hashLeaf(data, &result)
	return result[:]
}

func hashPairTest(left, right []byte) []byte {
	var l, r, result [32]byte
	copy(l[:], left)
	copy(r[:], right)
	hashPair(&l, &r, &result)
	return result[:]
}

func makeTestLeavesInternal(n int) [][]byte {
	leaves := make([][]byte, n)
	for i := range n {
		leaf := make([]byte, 32)
		for j := range 32 {
			leaf[j] = byte((i + j) % 256)
		}
		leaves[i] = leaf
	}
	return leaves
}
