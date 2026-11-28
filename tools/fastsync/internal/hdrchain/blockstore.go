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

// guard runs load and turns a store panic on an undecodable record into an error.
func guard[T any](what string, h int64, load func() (T, error)) (v T, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%s %d: %v", what, h, r)
		}
	}()
	return load()
}

// SignedHeader returns header h with the canonical commit for h (stored with
// block h+1), or the seen commit when h is the store's tip.
func (s *BlockstoreSource) SignedHeader(_ context.Context, h int64) (*types.SignedHeader, error) {
	return guard("blockstore height", h, func() (*types.SignedHeader, error) {
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
	})
}

// Validators returns the validator set for height h from state.db.
func (s *BlockstoreSource) Validators(_ context.Context, h int64) (*types.ValidatorSet, error) {
	if s.State == nil {
		return nil, fmt.Errorf("blockstore source has no state store for validators")
	}
	return guard("state store validators", h, func() (*types.ValidatorSet, error) { return s.State.LoadValidators(h) })
}
