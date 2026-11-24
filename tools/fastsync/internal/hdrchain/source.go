package hdrchain

import (
	"context"
	"fmt"

	"github.com/cometbft/cometbft/types"
)

// Source serves headers, commits and validator sets. It is untrusted: every
// value it returns is checked by the Verifier.
type Source interface {
	// SignedHeader returns header h and the commit for h. Below the source's
	// tip this must be the canonical commit (the LastCommit of block h+1).
	SignedHeader(ctx context.Context, h int64) (*types.SignedHeader, error)
	// Validators returns the validator set that signs height h.
	Validators(ctx context.Context, h int64) (*types.ValidatorSet, error)
}

// Stats counts what RunForward did.
type Stats struct {
	Headers       int64
	ValidatorSets int64 // validator sets fetched (only when ValidatorsHash changes)
}

// RunForward verifies heights from..to (inclusive) from src with v, which must
// be positioned at from-1 (or at the initial height in genesis mode). onBlock,
// if set, is called for every accepted signed header in height order.
func RunForward(ctx context.Context, src Source, v *Verifier, from, to int64, onBlock func(*types.SignedHeader) error) (Stats, error) {
	var st Stats
	if last := v.Last(); last != nil && last.Height != from-1 {
		return st, fmt.Errorf("verifier is at height %d, want %d", last.Height, from-1)
	}
	for h := from; h <= to; h++ {
		if err := ctx.Err(); err != nil {
			return st, err
		}
		sh, err := src.SignedHeader(ctx, h)
		if err != nil {
			return st, fmt.Errorf("height %d: %w", h, err)
		}
		if sh == nil || sh.Header == nil || sh.Height != h {
			return st, fmt.Errorf("source returned wrong or empty header for height %d", h)
		}
		var vals *types.ValidatorSet
		if v.NeedValidators(sh) {
			if vals, err = src.Validators(ctx, h); err != nil {
				return st, fmt.Errorf("validators %d: %w", h, err)
			}
			st.ValidatorSets++
		}
		if err := v.Verify(sh, vals); err != nil {
			return st, err
		}
		st.Headers++
		if onBlock != nil {
			if err := onBlock(sh); err != nil {
				return st, err
			}
		}
	}
	return st, nil
}
