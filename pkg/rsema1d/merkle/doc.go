// Package merkle implements a binary SHA-256 Merkle tree that is wire- and
// proof-compatible with CometBFT/Tendermint: leaves hash as sha256(0x00||leaf)
// and internal nodes as sha256(0x01||left||right).
//
// [NewTree] builds a tree over a power-of-2 number of leaves into one flat,
// contiguous node buffer of [TreeBufferSize] bytes. The caller may pass that
// buffer, e.g. from a pool, or nil to allocate it; the tree and every proof it
// hands out alias it.
//
// [Tree.Proof] and [Tree.Proofs] produce inclusion proofs, which
// [RootFromProof] and [RootFromProofs] verify.
package merkle
