// Package testchain builds small synthetic CometBFT chains for tests: real
// blocks, headers that hash-link, commits signed by the validator set, and
// validator sets that change from height to height.
package testchain

import (
	"crypto/sha256"
	"fmt"
	"testing"
	"time"

	dbm "github.com/cometbft/cometbft-db"
	cmtstate "github.com/cometbft/cometbft/proto/tendermint/state"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	cmtversion "github.com/cometbft/cometbft/proto/tendermint/version"
	sm "github.com/cometbft/cometbft/state"
	"github.com/cometbft/cometbft/store"
	"github.com/cometbft/cometbft/types"
	"github.com/cometbft/cometbft/version"
	"github.com/stretchr/testify/require"
)

// ChainID is the chain id of every test chain.
const ChainID = "fastsync-test"

// AppVersion is the app version in every header.
const AppVersion = 3

// Chain is a synthetic chain of blocks 1..N. Slices are indexed by height.
type Chain struct {
	GenDoc   *types.GenesisDoc
	Params   types.ConsensusParams
	Blocks   []*types.Block
	Parts    []*types.PartSet
	Commits  []*types.Commit       // Commits[h] signs block h
	Vals     []*types.ValidatorSet // Vals[h] is the validator set of height h, for 1..N+1
	PrivVals []types.PrivValidator
}

// N is the last height of the chain.
func (c *Chain) N() int64 { return int64(len(c.Blocks) - 1) }

// Header returns the header of block h.
func (c *Chain) Header(h int64) *types.Header { return &c.Blocks[h].Header }

// BlockID returns the block id of block h.
func (c *Chain) BlockID(h int64) types.BlockID {
	return types.BlockID{Hash: c.Blocks[h].Hash(), PartSetHeader: c.Parts[h].Header()}
}

// New builds a chain of n blocks signed by four validators whose voting
// powers change with the height, so that consecutive validator sets differ.
func New(t testing.TB, n int) *Chain {
	t.Helper()
	pvs := make([]types.PrivValidator, 4)
	for i := range pvs {
		pvs[i] = types.NewMockPV()
	}
	genTime := time.Now().Add(-2 * time.Hour).UTC().Truncate(time.Millisecond)
	params := *types.DefaultConsensusParams()
	params.Version.App = AppVersion
	c := &Chain{
		Params:   params,
		Blocks:   make([]*types.Block, n+1),
		Parts:    make([]*types.PartSet, n+1),
		Commits:  make([]*types.Commit, n+1),
		Vals:     make([]*types.ValidatorSet, n+2),
		PrivVals: pvs,
	}
	for h := 1; h <= n+1; h++ {
		c.Vals[h] = valset(t, pvs, int64(h))
	}
	c.GenDoc = &types.GenesisDoc{
		GenesisTime:     genTime,
		ChainID:         ChainID,
		InitialHeight:   1,
		ConsensusParams: &params,
		AppHash:         []byte{},
	}
	for _, v := range c.Vals[1].Validators {
		c.GenDoc.Validators = append(c.GenDoc.Validators, types.GenesisValidator{
			Address: v.Address, PubKey: v.PubKey, Power: v.VotingPower,
		})
	}
	require.NoError(t, c.GenDoc.ValidateAndComplete())

	lastCommit := &types.Commit{}
	var lastID types.BlockID
	for h := int64(1); h <= int64(n); h++ {
		b := types.MakeBlock(h, types.Data{Txs: types.MakeTxs(h, 2)}, lastCommit, nil)
		b.Version = cmtversion.Consensus{Block: version.BlockProtocol, App: AppVersion}
		b.ChainID = ChainID
		b.Time = genTime.Add(time.Duration(h) * time.Second)
		b.LastBlockID = lastID
		b.ValidatorsHash = c.Vals[h].Hash()
		b.NextValidatorsHash = c.Vals[h+1].Hash()
		b.ConsensusHash = params.Hash()
		b.AppHash = Hash("app", h)
		b.LastResultsHash = Hash("results", h)
		b.ProposerAddress = c.Vals[h].GetProposer().Address
		require.NoError(t, b.ValidateBasic(), "block %d", h)
		ps, err := b.MakePartSet(types.BlockPartSizeBytes)
		require.NoError(t, err)
		c.Blocks[h], c.Parts[h] = b, ps
		id := c.BlockID(h)
		c.Commits[h] = sign(t, c.Vals[h], pvs, id, h, b.Time.Add(500*time.Millisecond))
		lastCommit, lastID = c.Commits[h], id
	}
	return c
}

// Hash returns a deterministic 32-byte value for tests.
func Hash(tag string, h int64) []byte {
	s := sha256.Sum256(fmt.Appendf(nil, "%s/%d", tag, h))
	return s[:]
}

func valset(t testing.TB, pvs []types.PrivValidator, h int64) *types.ValidatorSet {
	vals := make([]*types.Validator, len(pvs))
	for i, pv := range pvs {
		pk, err := pv.GetPubKey()
		require.NoError(t, err)
		vals[i] = types.NewValidator(pk, 10+int64(i)+(h%3)*int64(i))
	}
	return types.NewValidatorSet(vals)
}

func sign(t testing.TB, vs *types.ValidatorSet, pvs []types.PrivValidator, id types.BlockID, h int64, ts time.Time) *types.Commit {
	byAddr := map[string]types.PrivValidator{}
	for _, pv := range pvs {
		pk, err := pv.GetPubKey()
		require.NoError(t, err)
		byAddr[string(pk.Address())] = pv
	}
	ordered := make([]types.PrivValidator, len(vs.Validators))
	for i, v := range vs.Validators {
		ordered[i] = byAddr[string(v.Address)]
	}
	voteSet := types.NewVoteSet(ChainID, h, 0, cmtproto.PrecommitType, vs)
	ec, err := types.MakeExtCommit(id, h, 0, voteSet, ordered, ts, false)
	require.NoError(t, err)
	return ec.ToCommit()
}

// State returns the CometBFT state after height h as a light-client state sync builds it.
func (c *Chain) State(h int64) sm.State {
	next := c.Header(h + 1)
	return sm.State{
		Version:                          cmtstate.Version{Consensus: next.Version, Software: version.TMCoreSemVer},
		ChainID:                          ChainID,
		InitialHeight:                    1,
		LastBlockHeight:                  h,
		LastBlockID:                      c.Commits[h].BlockID,
		LastBlockTime:                    c.Header(h).Time,
		NextValidators:                   c.Vals[h+2],
		Validators:                       c.Vals[h+1],
		LastValidators:                   c.Vals[h],
		LastHeightValidatorsChanged:      h + 2,
		ConsensusParams:                  c.Params,
		LastHeightConsensusParamsChanged: h + 1,
		LastResultsHash:                  next.LastResultsHash,
		AppHash:                          next.AppHash,
	}
}

// WriteStores writes blocks 1..blocksTo into dir/blockstore.db and the state
// after stateAt into dir/state.db (goleveldb), as a node that synced them would.
func (c *Chain) WriteStores(t testing.TB, dir string, blocksTo, stateAt int64) {
	t.Helper()
	bdb, err := dbm.NewDB("blockstore", dbm.GoLevelDBBackend, dir)
	require.NoError(t, err)
	bs := store.NewBlockStore(bdb)
	for h := int64(1); h <= blocksTo; h++ {
		bs.SaveBlock(c.Blocks[h], c.Parts[h], c.Commits[h])
	}
	require.NoError(t, bs.Close())
	sdb, err := dbm.NewDB("state", dbm.GoLevelDBBackend, dir)
	require.NoError(t, err)
	ss := sm.NewStore(sdb, sm.StoreOptions{})
	// Each Bootstrap stores the validator sets of h, h+1, h+2, the params of h+1
	// and the state after h; the last one leaves the state at stateAt.
	for h := int64(1); h <= stateAt; h++ {
		require.NoError(t, ss.Bootstrap(c.State(h)))
	}
	require.NoError(t, ss.Close())
}
