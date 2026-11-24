// Package boot bootstraps CometBFT's state store and block store at height H
// from data checked against one trusted value, the hash of header H+1, with
// no light client and no trusting period.
//
// It writes exactly what node.BootstrapStateWithGenProvider (celestia-core,
// the `comet bootstrap-state` command) writes, through the same exported
// store calls: the genesis doc, the seen commit for H, and the state after H
// with the validator sets of H, H+1, H+2, the consensus params of H+1 and the
// offline state sync height H.
package boot

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	cfg "github.com/cometbft/cometbft/config"
	"github.com/cometbft/cometbft/libs/log"
	"github.com/cometbft/cometbft/node"
	cmtstate "github.com/cometbft/cometbft/proto/tendermint/state"
	sm "github.com/cometbft/cometbft/state"
	"github.com/cometbft/cometbft/store"
	"github.com/cometbft/cometbft/types"
	"github.com/cometbft/cometbft/version"
)

// Verified is the data for a bootstrap at Height, each item checked against
// the trusted hash of header Height+1.
type Verified struct {
	Height     int64
	Header     *types.Header // header H
	Next       *types.Header // header H+1
	Vals       [3]*types.ValidatorSet
	Commit     *types.Commit
	Params     types.ConsensusParams
	Canonical  bool // Commit is block H+1's LastCommit (else a valid seen commit)
	SourceName string
}

// Verify fetches the bootstrap data for height h from src and checks it:
//
//	header h+1        hash == trustedNext
//	header h          hash == header(h+1).LastBlockID.Hash
//	validators h      Hash() == header(h).ValidatorsHash
//	validators h+1    Hash() == header(h+1).ValidatorsHash == header(h).NextValidatorsHash
//	validators h+2    Hash() == header(h+1).NextValidatorsHash
//	commit h          BlockID == header(h+1).LastBlockID, every signature valid, +2/3 of validators h
//	params h+1        Hash() == header(h+1).ConsensusHash
func Verify(ctx context.Context, src Source, chainID string, h int64, trustedNext []byte) (*Verified, error) {
	if h < 1 || len(trustedNext) != 32 {
		return nil, errors.New("need height >= 1 and a 32-byte trusted hash")
	}
	next, err := src.Header(ctx, h+1)
	if err != nil {
		return nil, err
	}
	if err := checkHeader(next, chainID, h+1); err != nil {
		return nil, err
	}
	if got := next.Hash(); !bytes.Equal(got, trustedNext) {
		return nil, fmt.Errorf("header %d: hash %X != trusted %X", h+1, got, trustedNext)
	}
	hdr, err := src.Header(ctx, h)
	if err != nil {
		return nil, err
	}
	if err := checkHeader(hdr, chainID, h); err != nil {
		return nil, err
	}
	if got := hdr.Hash(); !bytes.Equal(got, next.LastBlockID.Hash) {
		return nil, fmt.Errorf("header %d: hash %X != header(%d).LastBlockID.Hash %X", h, got, h+1, next.LastBlockID.Hash)
	}
	v := &Verified{Height: h, Header: hdr, Next: next, SourceName: src.String()}

	want := [3][][]byte{
		{hdr.ValidatorsHash},
		{next.ValidatorsHash, hdr.NextValidatorsHash},
		{next.NextValidatorsHash},
	}
	for i := range v.Vals {
		vs, err := src.Validators(ctx, h+int64(i))
		if err != nil {
			return nil, err
		}
		if vs == nil || vs.IsNilOrEmpty() {
			return nil, fmt.Errorf("validators %d: empty", h+int64(i))
		}
		if err := vs.ValidateBasic(); err != nil {
			return nil, fmt.Errorf("validators %d: %w", h+int64(i), err)
		}
		for _, w := range want[i] {
			if got := vs.Hash(); !bytes.Equal(got, w) {
				return nil, fmt.Errorf("validators %d: hash %X != %X from the verified headers", h+int64(i), got, w)
			}
		}
		v.Vals[i] = vs
	}

	if v.Commit, err = src.Commit(ctx, h); err != nil {
		return nil, err
	}
	if v.Commit == nil || v.Commit.Height != h {
		return nil, fmt.Errorf("commit: missing or not for height %d", h)
	}
	if err := v.Commit.ValidateBasic(); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}
	if !v.Commit.BlockID.Equals(next.LastBlockID) {
		return nil, fmt.Errorf("commit: BlockID %v != header(%d).LastBlockID %v", v.Commit.BlockID, h+1, next.LastBlockID)
	}
	// The stock state provider verifies every signature (VerifyCommit), not only +2/3.
	if err := v.Vals[0].VerifyCommit(chainID, v.Commit.BlockID, h, v.Commit); err != nil {
		return nil, fmt.Errorf("commit: not signed by validators %d: %w", h, err)
	}
	v.Canonical = bytes.Equal(v.Commit.Hash(), next.LastCommitHash)

	if v.Params, err = src.Params(ctx, h+1); err != nil {
		return nil, err
	}
	if err := v.Params.ValidateBasic(); err != nil {
		return nil, fmt.Errorf("consensus params %d: %w", h+1, err)
	}
	if got := v.Params.Hash(); !bytes.Equal(got, next.ConsensusHash) {
		return nil, fmt.Errorf("consensus params %d: hash %X != header(%d).ConsensusHash %X", h+1, got, h+1, next.ConsensusHash)
	}
	return v, nil
}

func checkHeader(h *types.Header, chainID string, height int64) error {
	if h == nil {
		return fmt.Errorf("header %d: missing", height)
	}
	if h.Height != height {
		return fmt.Errorf("header %d: source returned height %d", height, h.Height)
	}
	if h.ChainID != chainID {
		return fmt.Errorf("header %d: chain id %q != %q", height, h.ChainID, chainID)
	}
	if err := h.ValidateBasic(); err != nil {
		return fmt.Errorf("header %d: %w", height, err)
	}
	return nil
}

// State returns the state after v.Height, field by field as the stock
// light-client state provider builds it.
func (v *Verified) State(genState sm.State) sm.State {
	state := sm.State{
		ChainID:       genState.ChainID,
		Version:       genState.Version,
		InitialHeight: genState.InitialHeight,
	}
	if state.InitialHeight == 0 {
		state.InitialHeight = 1
	}
	state.Version = cmtstate.Version{Consensus: v.Next.Version, Software: version.TMCoreSemVer}
	state.LastBlockHeight = v.Header.Height
	state.LastBlockTime = v.Header.Time
	state.LastBlockID = v.Commit.BlockID
	state.AppHash = v.Next.AppHash
	state.LastResultsHash = v.Next.LastResultsHash
	state.LastValidators = v.Vals[0]
	state.Validators = v.Vals[1]
	state.NextValidators = v.Vals[2]
	state.LastHeightValidatorsChanged = v.Height + 2
	state.ConsensusParams = v.Params
	state.LastHeightConsensusParamsChanged = v.Height + 1
	return state
}

// Write stores v in the node home described by conf, mirroring
// node.BootstrapStateWithGenProvider: same preconditions (empty block store,
// empty state), same calls, same order.
func Write(conf *cfg.Config, genProvider node.GenesisDocProvider, v *Verified) (err error) {
	bsDB, err := cfg.DefaultDBProvider(&cfg.DBContext{ID: "blockstore", Config: conf, Path: conf.BlockstoreDir()})
	if err != nil {
		return err
	}
	blockStore := store.NewBlockStore(bsDB,
		store.WithCompaction(conf.Storage.Compact, conf.Storage.CompactionInterval),
		store.WithLogger(log.NewNopLogger()))
	defer func() { err = errors.Join(err, blockStore.Close()) }()
	stateDB, err := cfg.DefaultDBProvider(&cfg.DBContext{ID: "state", Config: conf, Path: conf.DBDir()})
	if err != nil {
		return err
	}
	if !blockStore.IsEmpty() {
		_ = stateDB.Close()
		return errors.New("blockstore not empty, trying to initialize non empty state")
	}
	stateStore := sm.NewStore(stateDB, sm.StoreOptions{DiscardABCIResponses: conf.Storage.DiscardABCIResponses})
	defer func() { err = errors.Join(err, stateStore.Close()) }()
	cur, err := stateStore.Load()
	if err != nil {
		return err
	}
	if !cur.IsEmpty() {
		return errors.New("state not empty, trying to initialize non empty state")
	}
	genState, _, err := node.LoadStateFromDBOrGenesisDocProvider(stateDB, genProvider)
	if err != nil {
		return err
	}
	if genState.ChainID != v.Next.ChainID {
		return fmt.Errorf("genesis chain id %q != header chain id %q", genState.ChainID, v.Next.ChainID)
	}
	state := v.State(genState)
	// The seen commit goes first: once the state is bootstrapped, a restart needs it.
	if err := blockStore.SaveSeenCommit(state.LastBlockHeight, v.Commit); err != nil {
		return err
	}
	// Bootstrap also stores the offline state sync height, so block sync starts at H+1.
	return stateStore.Bootstrap(state)
}
