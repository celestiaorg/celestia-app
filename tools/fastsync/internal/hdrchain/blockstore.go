package hdrchain

import (
	"context"
	"fmt"

	sm "github.com/cometbft/cometbft/state"
	"github.com/cometbft/cometbft/store"
	"github.com/cometbft/cometbft/types"
)

// BlockstoreSource reads from a node's blockstore.db and state.db. A node
// synced from genesis with the multiplexer writes both with one celestia-core
// for every height, and the stored formats did not change between v0.34 and
// v0.38 (keys H:<h>, C:<h>, SC:<h>; validatorsKey:<h> in state.db).
// The databases are untrusted like any other source.
type BlockstoreSource struct {
	Blocks *store.BlockStore
	State  sm.Store // optional: needed only for Validators
}

// SignedHeader returns header h with the canonical commit for h (stored with
// block h+1), or the seen commit when h is the store's tip.
func (s *BlockstoreSource) SignedHeader(_ context.Context, h int64) (sh *types.SignedHeader, err error) {
	// The store panics on undecodable records; corrupted input is an error here.
	defer func() {
		if r := recover(); r != nil {
			sh, err = nil, fmt.Errorf("blockstore height %d: %v", h, r)
		}
	}()
	meta := s.Blocks.LoadBlockMeta(h)
	if meta == nil {
		return nil, fmt.Errorf("blockstore: no block meta at %d", h)
	}
	commit := s.Blocks.LoadBlockCommit(h)
	if commit == nil {
		commit = s.Blocks.LoadSeenCommit(h)
	}
	if commit == nil {
		return nil, fmt.Errorf("blockstore: no commit for %d", h)
	}
	hd := meta.Header
	return &types.SignedHeader{Header: &hd, Commit: commit}, nil
}

// Validators returns the validator set for height h from state.db.
func (s *BlockstoreSource) Validators(_ context.Context, h int64) (vals *types.ValidatorSet, err error) {
	if s.State == nil {
		return nil, fmt.Errorf("blockstore source has no state store for validators")
	}
	defer func() {
		if r := recover(); r != nil {
			vals, err = nil, fmt.Errorf("state store validators %d: %v", h, r)
		}
	}()
	return s.State.LoadValidators(h)
}
