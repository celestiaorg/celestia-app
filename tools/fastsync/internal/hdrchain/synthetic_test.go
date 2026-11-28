package hdrchain

import (
	"bytes"
	"context"
	"testing"
	"time"

	dbm "github.com/cometbft/cometbft-db"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	"github.com/cometbft/cometbft/store"
	"github.com/cometbft/cometbft/types"
	"github.com/stretchr/testify/require"

	"github.com/celestiaorg/celestia-app/tools/fastsync/internal/testchain"
)

// chainSource serves headers and commits from a block store and validator
// sets from the chain, optionally replacing some.
type chainSource struct {
	*BlockstoreSource
	c    *testchain.Chain
	vals map[int64]*types.ValidatorSet
}

func newChainSource(c *testchain.Chain) chainSource {
	return chainSource{&BlockstoreSource{Blocks: c.MemBlockStore()}, c, map[int64]*types.ValidatorSet{}}
}

func (s chainSource) Validators(_ context.Context, h int64) (*types.ValidatorSet, error) {
	if v, ok := s.vals[h]; ok {
		return v.Copy(), nil
	}
	return s.c.Vals[h].Copy(), nil
}

func TestSyntheticForwardFromGenesis(t *testing.T) {
	c := testchain.New(t, 12, testchain.ValidatorsChangeAt(7), testchain.AppVersionFrom(5))
	src := newChainSource(c)
	v, err := NewGenesisVerifier(testchain.ChainID, 1, c.Vals[1].Hash())
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
	require.Equal(t, c.Commits[12].Hash(), sh.Commit.Hash())

	// A wrong genesis root.
	v, err = NewGenesisVerifier(testchain.ChainID, 1, bytes.Repeat([]byte{1}, 32))
	require.NoError(t, err)
	_, err = RunForward(context.Background(), src, v, 1, 1, nil)
	require.ErrorContains(t, err, "genesis validator set hash")
}

func TestSyntheticForwardWithChangingSets(t *testing.T) {
	c := testchain.New(t, 8) // a different set at every height
	v, err := NewGenesisVerifier(testchain.ChainID, 1, c.Vals[1].Hash())
	require.NoError(t, err)
	st, err := RunForward(context.Background(), newChainSource(c), v, 1, 8, nil)
	require.NoError(t, err)
	require.EqualValues(t, 8, st.ValidatorSets)
}

func TestSyntheticRejectsValidatorSwap(t *testing.T) {
	c := testchain.New(t, 8, testchain.ValidatorsChangeAt(5))
	// Serve the old set at the switch height: the header names the new one.
	src := newChainSource(c)
	src.vals[5] = c.Vals[4]
	v, err := NewGenesisVerifier(testchain.ChainID, 1, c.Vals[1].Hash())
	require.NoError(t, err)
	_, err = RunForward(context.Background(), src, v, 1, 8, nil)
	require.ErrorContains(t, err, "validator set hash")
	require.EqualValues(t, 4, v.Last().Height)
}

func TestSyntheticRejectsForgedHeader(t *testing.T) {
	c := testchain.New(t, 6, testchain.ValidatorsChangeAt(100))
	// A header at height 4 that links to 3 but is signed by a different set.
	forgerVals, forgerPrivs := types.RandValidatorSet(4, 10)
	byAddr := map[string]types.PrivValidator{}
	for _, pv := range forgerPrivs {
		pk, err := pv.GetPubKey()
		require.NoError(t, err)
		byAddr[string(pk.Address())] = pv
	}
	ordered := make([]types.PrivValidator, len(forgerVals.Validators))
	for i, val := range forgerVals.Validators {
		ordered[i] = byAddr[string(val.Address)]
	}
	h3 := c.Header(3)
	blk := types.MakeBlock(4, types.MakeData(nil), c.Commits[3], nil)
	blk.Version = h3.Version
	blk.ChainID = testchain.ChainID
	blk.Time = h3.Time.Add(time.Second)
	blk.LastBlockID = c.Commits[3].BlockID
	blk.ValidatorsHash = h3.NextValidatorsHash // claims the right set...
	blk.NextValidatorsHash = h3.NextValidatorsHash
	blk.ProposerAddress = forgerVals.Validators[0].Address
	ps, err := blk.MakePartSet(types.BlockPartSizeBytes)
	require.NoError(t, err)
	id := types.BlockID{Hash: blk.Hash(), PartSetHeader: ps.Header()}
	voteSet := types.NewVoteSet(testchain.ChainID, 4, 0, cmtproto.PrecommitType, forgerVals)
	ext, err := types.MakeExtCommit(id, 4, 0, voteSet, ordered, blk.Time, false)
	require.NoError(t, err)

	v, err := NewGenesisVerifier(testchain.ChainID, 1, c.Vals[1].Hash())
	require.NoError(t, err)
	_, err = RunForward(context.Background(), newChainSource(c), v, 1, 3, nil)
	require.NoError(t, err)
	forged := &types.SignedHeader{Header: &blk.Header, Commit: ext.ToCommit()}
	// ...but is signed by the forger: with the real set the commit fails,
	// with the forger's set the hash does not match.
	require.Error(t, v.Verify(forged, c.Vals[4]))
	require.ErrorContains(t, v.Verify(forged, forgerVals), "validator set hash")
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
	require.ErrorContains(t, err, "blockstore height 1")
}
