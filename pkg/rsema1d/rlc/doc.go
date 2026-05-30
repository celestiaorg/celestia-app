// Package rlc implements the random linear combination primitive used to
// commit to and verify rsema1d-encoded data.
//
// Public surface:
//
//   - [Derive]: Fiat-Shamir derivation of per-symbol coefficients from a
//     row Merkle root and the codec parameters.
//   - [Compute]: vectorized GF(2^16) SIMD computation of the RLC of every
//     row against the derived coefficients.
//   - [ComputeRow]: scalar per-row RLC, used by single-row verification
//     paths (standalone proofs) that don't justify Compute's setup cost.
//   - [Marshal] / [Unmarshal] / [Encode] / [Decode]: GF128 slab
//     serialization for RLC values on the wire.
package rlc
