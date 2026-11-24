package hdrchain

import (
	"context"
	"fmt"
	"sort"
	"testing"
	"time"

	dbm "github.com/cometbft/cometbft-db"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	cmtversion "github.com/cometbft/cometbft/proto/tendermint/version"
	"github.com/cometbft/cometbft/store"
	"github.com/cometbft/cometbft/types"
	"github.com/stretchr/testify/require"
)

const testChainID = "test-chain"

type valSet struct {
	vals  *types.ValidatorSet
	privs []types.PrivValidator // in vals order
}

// newValSet returns n equal-power validators; vals and privs share an order.
func newValSet(t testing.TB, n int) valSet {
	var vs []*types.Validator
	var ps []types.PrivValidator
	for i := 0; i < n; i++ {
		v, p := types.RandValidator(false, 10)
		vs = append(vs, v)
		ps = append(ps, p)
	}
	set := types.NewValidatorSet(vs)
	sort.Sort(types.PrivValidatorsByAddress(ps)) // equal power: canonical order is by address
	for i, v := range set.Validators {
		pk, err := ps[i].GetPubKey()
		require.NoError(t, err)
		require.Equal(t, v.Address, pk.Address())
	}
	return valSet{set, ps}
}

type synthChain struct {
	blocks  []*types.Block // index h-1
	commits []*types.Commit
	vals    map[int64]*types.ValidatorSet
	bs      *store.BlockStore
}

// buildChain makes heights 1..n. Validators switch from set A to set B at
// switchAt (B announced in header switchAt-1's NextValidatorsHash). App
// version goes 3 -> 4 at appSwitch.
func buildChain(t testing.TB, n, switchAt, appSwitch int64) *synthChain {
	a, b := newValSet(t, 4), newValSet(t, 5)
	setAt := func(h int64) valSet {
		if h >= switchAt {
			return b
		}
		return a
	}
	c := &synthChain{vals: map[int64]*types.ValidatorSet{}, bs: store.NewBlockStore(dbm.NewMemDB())}
	start := time.Date(2023, 10, 31, 14, 0, 0, 0, time.UTC)
	lastCommit := &types.Commit{}
	var lastID types.BlockID
	for h := int64(1); h <= n; h++ {
		cur, next := setAt(h), setAt(h+1)
		blk := types.MakeBlock(h, types.MakeData(types.MakeTxs(h, 2)), lastCommit, nil)
		app := uint64(3)
		if h >= appSwitch {
			app = 4
		}
		blk.Version = cmtversion.Consensus{Block: BlockProtocol, App: app}
		blk.ChainID = testChainID
		blk.Time = start.Add(time.Duration(h) * 6 * time.Second)
		blk.LastBlockID = lastID
		blk.ValidatorsHash = cur.vals.Hash()
		blk.NextValidatorsHash = next.vals.Hash()
		blk.ConsensusHash = make([]byte, 32)
		blk.AppHash = make([]byte, 32)
		blk.LastResultsHash = make([]byte, 32)
		blk.ProposerAddress = cur.vals.Validators[0].Address
		ps, err := blk.MakePartSet(types.BlockPartSizeBytes)
		require.NoError(t, err)
		id := types.BlockID{Hash: blk.Hash(), PartSetHeader: ps.Header()}
		voteSet := types.NewVoteSet(testChainID, h, 0, cmtproto.PrecommitType, cur.vals)
		ext, err := types.MakeExtCommit(id, h, 0, voteSet, cur.privs, blk.Time.Add(time.Second), false)
		require.NoError(t, err)
		commit := ext.ToCommit()
		c.bs.SaveBlock(blk, ps, commit)
		c.blocks = append(c.blocks, blk)
		c.commits = append(c.commits, commit)
		c.vals[h] = cur.vals
		lastCommit, lastID = commit, id
	}
	return c
}

// mixedSource: headers/commits from the blockstore, validators from a map.
type mixedSource struct {
	*BlockstoreSource
	vals map[int64]*types.ValidatorSet
}

func (m mixedSource) Validators(_ context.Context, h int64) (*types.ValidatorSet, error) {
	v, ok := m.vals[h]
	if !ok {
		return nil, fmt.Errorf("no validators at %d", h)
	}
	return v.Copy(), nil
}

func TestSyntheticForwardWithValidatorChange(t *testing.T) {
	c := buildChain(t, 12, 7, 5)
	src := mixedSource{&BlockstoreSource{Blocks: c.bs}, c.vals}
	v, err := NewGenesisVerifier(testChainID, 1, c.vals[1].Hash())
	require.NoError(t, err)
	v.AllSignatures = true
	var app []int64
	v.OnAppVersion = func(h int64, _, _ uint64) { app = append(app, h) }
	st, err := RunForward(context.Background(), src, v, 1, 12, nil)
	require.NoError(t, err)
	require.EqualValues(t, 12, st.Headers)
	require.EqualValues(t, 2, st.ValidatorSets, "fetched at genesis and at the switch only")
	require.Equal(t, []int64{5}, app)
	// The tip has no canonical commit yet; the seen commit is served.
	sh, err := src.SignedHeader(context.Background(), 12)
	require.NoError(t, err)
	require.Equal(t, c.commits[11].Hash(), sh.Commit.Hash())
}

func TestSyntheticRejectsValidatorSwap(t *testing.T) {
	c := buildChain(t, 8, 5, 100)
	// Serve the old set at the switch height: the header names the new one.
	vals := map[int64]*types.ValidatorSet{}
	for h, vs := range c.vals {
		vals[h] = vs
	}
	vals[5] = c.vals[4]
	src := mixedSource{&BlockstoreSource{Blocks: c.bs}, vals}
	v, err := NewGenesisVerifier(testChainID, 1, c.vals[1].Hash())
	require.NoError(t, err)
	_, err = RunForward(context.Background(), src, v, 1, 8, nil)
	require.ErrorContains(t, err, "validator set hash")
	require.EqualValues(t, 4, v.Last().Height)
}

func TestSyntheticRejectsForgedHeader(t *testing.T) {
	c := buildChain(t, 6, 100, 100)
	// A header at height 4 that links to 3 but is signed by a different set.
	forger := newValSet(t, 4)
	h3 := c.blocks[2]
	blk := types.MakeBlock(4, types.MakeData(nil), c.commits[2], nil)
	blk.Version = h3.Version
	blk.ChainID = testChainID
	blk.Time = h3.Time.Add(time.Second)
	blk.LastBlockID = c.commits[2].BlockID
	blk.ValidatorsHash = h3.NextValidatorsHash // claims the right set...
	blk.NextValidatorsHash = h3.NextValidatorsHash
	blk.ProposerAddress = forger.vals.Validators[0].Address
	ps, err := blk.MakePartSet(types.BlockPartSizeBytes)
	require.NoError(t, err)
	id := types.BlockID{Hash: blk.Hash(), PartSetHeader: ps.Header()}
	voteSet := types.NewVoteSet(testChainID, 4, 0, cmtproto.PrecommitType, forger.vals)
	ext, err := types.MakeExtCommit(id, 4, 0, voteSet, forger.privs, blk.Time, false)
	require.NoError(t, err)

	v, err := NewGenesisVerifier(testChainID, 1, c.vals[1].Hash())
	require.NoError(t, err)
	src := mixedSource{&BlockstoreSource{Blocks: c.bs}, c.vals}
	_, err = RunForward(context.Background(), src, v, 1, 3, nil)
	require.NoError(t, err)
	forged := &types.SignedHeader{Header: &blk.Header, Commit: ext.ToCommit()}
	// ...but is signed by the forger: with the real set the commit fails,
	// with the forger's set the hash does not match.
	require.Error(t, v.Verify(forged, c.vals[4]))
	require.ErrorContains(t, v.Verify(forged, forger.vals), "validator set hash")
}

func TestBlockstoreSourceMissing(t *testing.T) {
	src := &BlockstoreSource{Blocks: store.NewBlockStore(dbm.NewMemDB())}
	_, err := src.SignedHeader(context.Background(), 1)
	require.ErrorContains(t, err, "no block meta")
	_, err = src.Validators(context.Background(), 1)
	require.Error(t, err)
}

func TestBlockstoreSourceCorrupt(t *testing.T) {
	db := dbm.NewMemDB()
	require.NoError(t, db.Set([]byte("H:1"), []byte{0xff, 0xff, 0xff}))
	src := &BlockstoreSource{Blocks: store.NewBlockStore(db)}
	_, err := src.SignedHeader(context.Background(), 1)
	require.Error(t, err)
}
