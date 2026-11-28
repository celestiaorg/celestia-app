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
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	sm "github.com/cometbft/cometbft/state"
	"github.com/cometbft/cometbft/store"
	"github.com/cometbft/cometbft/types"
	"github.com/cometbft/cometbft/version"

	"github.com/celestiaorg/celestia-app/tools/fastsync/internal/hdrchain"
)

// Source serves the data a bootstrap needs. It is untrusted: Verify checks
// everything it returns against the trusted hash of header H+1.
type Source interface {
	hdrchain.Source
	// Params returns the consensus params in effect at height h.
	Params(ctx context.Context, h int64) (types.ConsensusParams, error)
}

// Verified is the data for a bootstrap at Height, each item checked against
// the trusted hash of header Height+1.
type Verified struct {
	Height    int64
	Header    *types.Header // header H
	Next      *types.Header // header H+1
	Vals      [3]*types.ValidatorSet
	Commit    *types.Commit
	Params    types.ConsensusParams
	Canonical bool // Commit is block H+1's LastCommit (else a valid seen commit)
}

// Verify fetches the bootstrap data for height h and checks it:
//
//	headers h+1, h    hash chain from trustedNext (hdrchain.VerifyBackward) and CheckLink
//	commit h          its BlockID is header(h+1).LastBlockID, every signature valid, +2/3 of validators h
//	validators h, h+1 hash to the headers' ValidatorsHash (hdrchain.CheckCommit on h and h+1)
//	validators h+2    Hash() == header(h+1).NextValidatorsHash
//	params h+1        Hash() == header(h+1).ConsensusHash, Version.App == header(h+1).Version.App
//	proposer          validators h name header(h).ProposerAddress for a round up to the commit's
//
// Proposer priorities and the params other than block max bytes and max gas
// are not part of any hash: every source must return identical bytes for
// them, so a single source is trusted for those.
func Verify(ctx context.Context, srcs []Source, chainID string, h int64, trustedNext []byte) (*Verified, error) {
	if len(srcs) == 0 || h < 1 || len(trustedNext) != 32 {
		return nil, errors.New("need a source, height >= 1 and a 32-byte trusted hash")
	}
	var sh [2]*types.SignedHeader
	for i := range sh {
		var err error
		if sh[i], err = signedHeader(ctx, srcs, h+int64(i)); err != nil {
			return nil, err
		}
	}
	hdr, next := sh[0].Header, sh[1].Header
	if _, err := hdrchain.VerifyBackward(chainID, trustedNext, []*types.Header{next, hdr}); err != nil {
		return nil, err
	}
	ht, err := hdrchain.ForAppVersion(next.Version.Block, next.Version.App)
	if err != nil {
		return nil, err
	}
	if err := hdrchain.CheckLink(ht, hdr, next); err != nil {
		return nil, err
	}
	v := &Verified{Height: h, Header: hdr, Next: next, Commit: sh[0].Commit}
	for i := range v.Vals {
		if v.Vals[i], err = agreeingValidators(ctx, srcs, h+int64(i)); err != nil {
			return nil, err
		}
	}
	if !v.Commit.BlockID.Equals(next.LastBlockID) {
		return nil, fmt.Errorf("commit %d: BlockID %v != header(%d).LastBlockID %v", h, v.Commit.BlockID, h+1, next.LastBlockID)
	}
	// The stock state provider verifies every signature (VerifyCommit), not only +2/3.
	for i := range sh {
		if err := hdrchain.CheckCommit(ht, chainID, sh[i], v.Vals[i], true); err != nil {
			return nil, err
		}
	}
	if got := v.Vals[2].Hash(); !bytes.Equal(got, next.NextValidatorsHash) {
		return nil, fmt.Errorf("validators %d: hash %X != header(%d).NextValidatorsHash %X", h+2, got, h+1, next.NextValidatorsHash)
	}
	if err := checkProposer(v.Vals[0], hdr, v.Commit.Round); err != nil {
		return nil, err
	}
	v.Canonical = hdrchain.CheckCommitBinding(ht, v.Commit, next) == nil
	if v.Params, err = agreeingParams(ctx, srcs, h+1); err != nil {
		return nil, err
	}
	if err := v.Params.ValidateBasic(); err != nil {
		return nil, fmt.Errorf("consensus params %d: %w", h+1, err)
	}
	if got := v.Params.Hash(); !bytes.Equal(got, next.ConsensusHash) {
		return nil, fmt.Errorf("consensus params %d: hash %X != header(%d).ConsensusHash %X", h+1, got, h+1, next.ConsensusHash)
	}
	if v.Params.Version.App != next.Version.App {
		return nil, fmt.Errorf("consensus params %d: app version %d != header(%d) app version %d", h+1, v.Params.Version.App, h+1, next.Version.App)
	}
	return v, nil
}

// signedHeader returns header h and its commit from the first source that has them.
func signedHeader(ctx context.Context, srcs []Source, h int64) (*types.SignedHeader, error) {
	var errs []error
	for i, s := range srcs {
		sh, err := s.SignedHeader(ctx, h)
		if err == nil && (sh == nil || sh.Header == nil || sh.Commit == nil || sh.Height != h) {
			err = fmt.Errorf("wrong or empty header for height %d", h)
		}
		if err == nil {
			return sh, nil
		}
		errs = append(errs, fmt.Errorf("source %d: %w", i, err))
	}
	return nil, errors.Join(errs...)
}

// agreeingValidators returns validators h; every source must return the same bytes.
func agreeingValidators(ctx context.Context, srcs []Source, h int64) (*types.ValidatorSet, error) {
	var first *types.ValidatorSet
	var firstBz []byte
	for i, s := range srcs {
		vs, err := s.Validators(ctx, h)
		if err == nil && (vs == nil || vs.IsNilOrEmpty()) {
			err = errors.New("empty")
		}
		var bz []byte
		if err == nil {
			var pb *cmtproto.ValidatorSet
			if pb, err = vs.ToProto(); err == nil {
				bz, err = pb.Marshal()
			}
		}
		if err != nil {
			return nil, fmt.Errorf("validators %d from source %d: %w", h, i, err)
		}
		if i == 0 {
			first, firstBz = vs, bz
		} else if !bytes.Equal(bz, firstBz) {
			return nil, fmt.Errorf("validators %d: sources 0 and %d disagree (proposer priorities and order are not covered by the header)", h, i)
		}
	}
	return first, nil
}

// agreeingParams returns params h; every source must return the same bytes.
func agreeingParams(ctx context.Context, srcs []Source, h int64) (types.ConsensusParams, error) {
	var first types.ConsensusParams
	var firstBz []byte
	for i, s := range srcs {
		p, err := s.Params(ctx, h)
		if err != nil {
			return first, fmt.Errorf("consensus params %d from source %d: %w", h, i, err)
		}
		pb := p.ToProto()
		bz, err := pb.Marshal()
		if err != nil {
			return first, err
		}
		if i == 0 {
			first, firstBz = p, bz
		} else if !bytes.Equal(bz, firstBz) {
			return first, fmt.Errorf("consensus params %d: sources 0 and %d disagree (fields not covered by ConsensusHash)", h, i)
		}
	}
	return first, nil
}

// checkProposer checks that vals, with the priorities the source gave, name
// the header's proposer in some round up to the commit's round (a block can
// be re-proposed in a later round). The reported priorities are those after
// round 0's proposer was chosen (vals.Proposer); later rounds increment them.
func checkProposer(vals *types.ValidatorSet, hdr *types.Header, round int32) error {
	vs := vals.Copy()
	for r := int32(0); r <= round; r++ {
		if r > 0 {
			vs.IncrementProposerPriority(1)
		}
		if bytes.Equal(vs.GetProposer().Address, hdr.ProposerAddress) {
			return nil
		}
	}
	return fmt.Errorf("validators %d: proposer priorities do not name header %d's proposer %X in rounds 0..%d", hdr.Height, hdr.Height, hdr.ProposerAddress, round)
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
