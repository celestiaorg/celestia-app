// Package hdrchain verifies the Celestia header chain: header hashes, hash
// links between consecutive headers, commit signatures (+2/3 of voting power)
// and validator-set transitions.
//
// One header type covers all mainnet heights. Celestia mainnet ran
// celestia-core v0.34.x (ABCI 1) for app versions 1-3 and v0.38-based
// celestia-core (ABCI 2) from app version 4 (height 6,680,340). Header,
// BlockID, PartSetHeader, Commit, CommitSig, CanonicalVote, SimpleValidator and
// the version.Consensus proto messages are byte-identical between the two
// lines, and so are Header.Hash, Commit.Hash, ValidatorSet.Hash and
// VoteSignBytes. Vote extensions (ExtendedCommit) exist only in v0.38 and are
// disabled on Celestia, so they never appear in a canonical commit. The
// mainnet fixtures in testdata (headers from both lines) verify with these
// types; see HEADERS.md for how this was cross-checked against the v0.34 types.
//
// The package uses the celestia-core types that celestia-app main builds
// with (module github.com/cometbft/cometbft), so it decodes headers written
// in either era from RPC JSON or from a blockstore.
package hdrchain
