package hdrchain

import (
	"fmt"

	"github.com/cometbft/cometbft/types"
)

// HeaderType is the pluggable layer for one header/commit encoding. A chain
// that changes how headers are hashed or how commits are signed adds a new
// implementation and maps the app versions that use it in ForAppVersion.
type HeaderType interface {
	// Name identifies the encoding in logs and in header-file manifests.
	Name() string
	// HeaderHash is the block hash: what the next header's LastBlockID.Hash
	// and the commit's BlockID.Hash refer to.
	HeaderHash(h *types.Header) []byte
	// CommitHash is what the next header's LastCommitHash refers to.
	CommitHash(c *types.Commit) []byte
	// ValidatorSetHash is what Header.ValidatorsHash and
	// Header.NextValidatorsHash refer to.
	ValidatorSetHash(vals *types.ValidatorSet) []byte
	// VerifyCommit checks that vals signed sh with more than 2/3 of their
	// voting power. allSignatures also checks every signature present, not
	// only enough to pass 2/3.
	VerifyCommit(chainID string, vals *types.ValidatorSet, sh *types.SignedHeader, allSignatures bool) error
}

// CometBFT is the encoding used by celestia-core v0.34.x and every
// v0.38-based release (v1.5x.x-tm-v0.38.17, v0.42.x, v0.43.x): 14-leaf
// merkle header hash, protobuf CommitSig leaves, length-delimited
// CanonicalVote sign bytes, SimpleValidator leaves.
type CometBFT struct{}

var _ HeaderType = CometBFT{}

func (CometBFT) Name() string { return "cometbft-block11" }

func (CometBFT) HeaderHash(h *types.Header) []byte { return h.Hash() }

func (CometBFT) CommitHash(c *types.Commit) []byte { return c.Hash() }

func (CometBFT) ValidatorSetHash(vals *types.ValidatorSet) []byte { return vals.Hash() }

func (CometBFT) VerifyCommit(chainID string, vals *types.ValidatorSet, sh *types.SignedHeader, allSignatures bool) error {
	if allSignatures {
		return types.VerifyCommit(chainID, vals, sh.Commit.BlockID, sh.Height, sh.Commit)
	}
	return types.VerifyCommitLight(chainID, vals, sh.Commit.BlockID, sh.Height, sh.Commit)
}

// BlockProtocol is the block version every Celestia header carries. A header
// with a different value needs a new HeaderType.
const BlockProtocol uint64 = 11

// MaxKnownAppVersion is the highest app version this package has a header
// type for. Raise it only after checking that the new version's celestia-core
// keeps the encoding (or after adding a new HeaderType).
const MaxKnownAppVersion uint64 = 12

// ForAppVersion returns the header type for a header with the given block
// and app versions.
func ForAppVersion(block, app uint64) (HeaderType, error) {
	if block != BlockProtocol {
		return nil, fmt.Errorf("unknown block protocol %d (want %d)", block, BlockProtocol)
	}
	if app == 0 || app > MaxKnownAppVersion {
		return nil, fmt.Errorf("no header type for app version %d", app)
	}
	// v1-v3: celestia-core v0.34.x; v4+: v0.38-based. Same encoding.
	return CometBFT{}, nil
}
