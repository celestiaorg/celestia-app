package hdrchain

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/cometbft/cometbft/types"
)

// CheckLink checks that next directly follows prev: next.LastBlockID.Hash is
// the hash of prev, heights are consecutive, chain IDs match, the validator
// set of next is the one prev announced, time strictly increases and the app
// version does not decrease. prev must already be trusted.
func CheckLink(ht HeaderType, prev, next *types.Header) error {
	if prev == nil || next == nil {
		return errors.New("nil header")
	}
	if next.Height != prev.Height+1 {
		return fmt.Errorf("height %d does not follow %d", next.Height, prev.Height)
	}
	if next.ChainID != prev.ChainID {
		return fmt.Errorf("height %d: chain id %q, previous %q", next.Height, next.ChainID, prev.ChainID)
	}
	if got := ht.HeaderHash(prev); !bytes.Equal(next.LastBlockID.Hash, got) {
		return fmt.Errorf("height %d: LastBlockID.Hash %X is not the hash %X of header %d", next.Height, next.LastBlockID.Hash, got, prev.Height)
	}
	if !bytes.Equal(next.ValidatorsHash, prev.NextValidatorsHash) {
		return fmt.Errorf("height %d: ValidatorsHash %X, header %d announced NextValidatorsHash %X", next.Height, next.ValidatorsHash, prev.Height, prev.NextValidatorsHash)
	}
	if !next.Time.After(prev.Time) {
		return fmt.Errorf("height %d: time %v not after %v", next.Height, next.Time, prev.Time)
	}
	if next.Version.App < prev.Version.App {
		return fmt.Errorf("height %d: app version %d below previous %d", next.Height, next.Version.App, prev.Version.App)
	}
	return nil
}

// CheckCommitBinding checks that commit is the canonical commit for the
// header before next, i.e. the one next.LastCommitHash commits to.
func CheckCommitBinding(ht HeaderType, commit *types.Commit, next *types.Header) error {
	if commit == nil || next == nil {
		return errors.New("nil commit or header")
	}
	if commit.Height != next.Height-1 {
		return fmt.Errorf("commit height %d, next header %d", commit.Height, next.Height)
	}
	if got := ht.CommitHash(commit); !bytes.Equal(got, next.LastCommitHash) {
		return fmt.Errorf("commit %d hash %X, header %d LastCommitHash %X", commit.Height, got, next.Height, next.LastCommitHash)
	}
	return nil
}

// CheckCommit checks sh on its own: basic validity, chain ID, commit for this
// header, vals hash to sh.ValidatorsHash, and +2/3 of vals' power signed.
func CheckCommit(ht HeaderType, chainID string, sh *types.SignedHeader, vals *types.ValidatorSet, allSignatures bool) error {
	if sh == nil {
		return errors.New("nil signed header")
	}
	if err := sh.ValidateBasic(chainID); err != nil {
		return err
	}
	if err := vals.ValidateBasic(); err != nil {
		return fmt.Errorf("height %d: validators: %w", sh.Height, err)
	}
	if got := ht.ValidatorSetHash(vals); !bytes.Equal(got, sh.ValidatorsHash) {
		return fmt.Errorf("height %d: validator set hash %X, header ValidatorsHash %X", sh.Height, got, sh.ValidatorsHash)
	}
	if err := ht.VerifyCommit(chainID, vals, sh, allSignatures); err != nil {
		return fmt.Errorf("height %d: %w", sh.Height, err)
	}
	return nil
}

// Verifier verifies signed headers sequentially, height by height. Every
// accepted header is signed by +2/3 of a validator set whose hash the
// previous accepted header announced, starting from a trust root.
type Verifier struct {
	chainID string
	// AllSignatures checks every signature in each commit, not only 2/3.
	AllSignatures bool
	// OnAppVersion, if set, is called when the app version changes.
	OnAppVersion func(height int64, from, to uint64)

	initialHeight int64
	rootValsHash  []byte // genesis mode, until the first header is accepted

	last         *types.SignedHeader
	lastVals     *types.ValidatorSet
	lastValsHash []byte
}

// NewGenesisVerifier starts at the chain's first height. genesisValsHash is
// the trust root: the hash of the validator set InitChain produced (for
// Celestia, derived from the genesis gentxs, see the README).
func NewGenesisVerifier(chainID string, initialHeight int64, genesisValsHash []byte) (*Verifier, error) {
	if chainID == "" || initialHeight < 1 || len(genesisValsHash) != 32 {
		return nil, errors.New("need chain id, initial height >= 1 and a 32-byte validator set hash")
	}
	return &Verifier{chainID: chainID, initialHeight: initialHeight, rootValsHash: genesisValsHash}, nil
}

// NewTrustedVerifier starts after a trusted signed header (e.g. from a
// previous run or a weak-subjectivity checkpoint). trustedHash is the
// out-of-band hash of sh.Header; vals is its validator set.
func NewTrustedVerifier(chainID string, sh *types.SignedHeader, trustedHash []byte, vals *types.ValidatorSet) (*Verifier, error) {
	if sh == nil || sh.Header == nil {
		return nil, errors.New("nil trusted header")
	}
	ht, err := ForAppVersion(sh.Version.Block, sh.Version.App)
	if err != nil {
		return nil, err
	}
	if got := ht.HeaderHash(sh.Header); !bytes.Equal(got, trustedHash) {
		return nil, fmt.Errorf("trusted header %d hash %X, want %X", sh.Height, got, trustedHash)
	}
	if err := sh.ValidateBasic(chainID); err != nil {
		return nil, err
	}
	if err := vals.ValidateBasic(); err != nil {
		return nil, err
	}
	vh := ht.ValidatorSetHash(vals)
	if !bytes.Equal(vh, sh.ValidatorsHash) {
		return nil, fmt.Errorf("trusted validator set hash %X, header has %X", vh, sh.ValidatorsHash)
	}
	return &Verifier{chainID: chainID, last: sh, lastVals: vals, lastValsHash: vh}, nil
}

// Last returns the last accepted signed header (nil before the first).
func (v *Verifier) Last() *types.SignedHeader { return v.last }

// NeedValidators reports whether Verify needs a validator set for sh, or can
// reuse the last one (same ValidatorsHash).
func (v *Verifier) NeedValidators(sh *types.SignedHeader) bool {
	return v.lastVals == nil || sh == nil || sh.Header == nil || !bytes.Equal(sh.ValidatorsHash, v.lastValsHash)
}

// Verify accepts sh as the next height if it checks out. vals may be nil when
// NeedValidators(sh) is false.
func (v *Verifier) Verify(sh *types.SignedHeader, vals *types.ValidatorSet) error {
	if sh == nil || sh.Header == nil || sh.Commit == nil {
		return errors.New("nil signed header")
	}
	ht, err := ForAppVersion(sh.Version.Block, sh.Version.App)
	if err != nil {
		return fmt.Errorf("height %d: %w", sh.Height, err)
	}
	if v.last == nil {
		if sh.Height != v.initialHeight {
			return fmt.Errorf("first header height %d, want initial height %d", sh.Height, v.initialHeight)
		}
		if !bytes.Equal(sh.ValidatorsHash, v.rootValsHash) {
			return fmt.Errorf("height %d: ValidatorsHash %X is not the genesis validator set hash %X", sh.Height, sh.ValidatorsHash, v.rootValsHash)
		}
	} else {
		if err := CheckLink(ht, v.last.Header, sh.Header); err != nil {
			return err
		}
		if err := CheckCommitBinding(ht, v.last.Commit, sh.Header); err != nil {
			return fmt.Errorf("previous commit is not canonical: %w", err)
		}
	}
	if vals == nil {
		if v.NeedValidators(sh) {
			return fmt.Errorf("height %d: validator set changed, need validators", sh.Height)
		}
		vals = v.lastVals
	}
	if err := CheckCommit(ht, v.chainID, sh, vals, v.AllSignatures); err != nil {
		return err
	}
	if v.last != nil && v.OnAppVersion != nil && sh.Version.App != v.last.Version.App {
		v.OnAppVersion(sh.Height, v.last.Version.App, sh.Version.App)
	}
	v.last = sh
	if vals != v.lastVals {
		v.lastVals = vals
		v.lastValsHash = ht.ValidatorSetHash(vals)
	}
	return nil
}

// VerifyBackward checks a hash chain of headers below a trusted anchor:
// headers[0] must hash to anchorHash and each headers[i+1] must hash to
// headers[i].LastBlockID.Hash. No signatures are needed: collision resistance
// of the header hash makes every header the one the anchor's chain committed
// to. It returns the LastBlockID.Hash of the lowest header, which anchors the
// next window.
func VerifyBackward(chainID string, anchorHash []byte, headers []*types.Header) ([]byte, error) {
	expect := anchorHash
	for i, h := range headers {
		if h == nil {
			return nil, fmt.Errorf("nil header at index %d", i)
		}
		if i > 0 && h.Height != headers[i-1].Height-1 {
			return nil, fmt.Errorf("height %d does not precede %d", h.Height, headers[i-1].Height)
		}
		if h.ChainID != chainID {
			return nil, fmt.Errorf("height %d: chain id %q", h.Height, h.ChainID)
		}
		if err := h.ValidateBasic(); err != nil {
			return nil, fmt.Errorf("height %d: %w", h.Height, err)
		}
		ht, err := ForAppVersion(h.Version.Block, h.Version.App)
		if err != nil {
			return nil, fmt.Errorf("height %d: %w", h.Height, err)
		}
		if got := ht.HeaderHash(h); !bytes.Equal(got, expect) {
			return nil, fmt.Errorf("hash chain broken at height %d: header hash %X, expected %X", h.Height, got, expect)
		}
		expect = h.LastBlockID.Hash
	}
	return expect, nil
}
