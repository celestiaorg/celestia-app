// Package merkle implements a binary SHA-256 Merkle tree that is wire- and
// proof-compatible with CometBFT/Tendermint: leaves hash as sha256(0x00||leaf)
// and internal nodes as sha256(0x01||left||right).
//
// Trees are built over a power-of-2 number of leaves and stored as one flat,
// contiguous node buffer, so a tree can be sourced from a pooled byte region and
// built in parallel without reinterpretation. Construction comes in a few styles
// depending on how leaves are supplied and who owns the backing storage:
//
//   - [NewTree] hashes already-materialized leaves.
//   - [NewTreeInto] does the same into caller-owned storage.
//   - [NewTreeFuncInto] hashes leaves produced on demand by a callback into
//     caller-owned storage.
//
// [RootFromFunc] computes a root alone when no proofs are needed.
// [Tree.Proof] and [Tree.Proofs] produce inclusion proofs, which
// [RootFromProof] and [RootFromProofs] verify.
package merkle
