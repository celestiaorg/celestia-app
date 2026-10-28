package app_test

import (
	"errors"
	"fmt"
	"testing"

	"cosmossdk.io/log"
	"github.com/celestiaorg/celestia-app/v11/app"
	"github.com/celestiaorg/celestia-app/v11/test/util"
	minttypes "github.com/celestiaorg/celestia-app/v11/x/mint/types"
	abci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	tmdb "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/baseapp"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

// TestStateFixRepairsHaltedChain halts a chain on a missing minter, restarts it
// with a fix that restores the minter, and checks that a node syncing from
// genesis with the same fix reaches the same app hashes.
func TestStateFixRepairsHaltedChain(t *testing.T) {
	const corrupt = int64(3)
	fixHeight := corrupt + 1
	var ranAt []int64
	fixes := []app.StateFix{
		{
			ChainID: util.ChainID,
			Height:  fixHeight,
			Name:    "restore-minter",
			Apply: func(ctx sdk.Context, a *app.App) error {
				a.MintKeeper.SetMinter(ctx, minttypes.DefaultMinter())
				ranAt = append(ranAt, ctx.BlockHeight())
				return nil
			},
		},
		{
			ChainID: "other-chain",
			Height:  fixHeight,
			Name:    "other-chain",
			Apply: func(sdk.Context, *app.App) error {
				return errors.New("ran on the wrong chain")
			},
		},
	}

	// Node A halts at fixHeight without the fix.
	dbA := tmdb.NewMemDB()
	nodeA := newStateFixApp(t, dbA, nil, true)
	hashes := make(map[int64][]byte)
	for h := int64(1); h <= corrupt; h++ {
		hashes[h] = runStateFixBlock(t, nodeA, h, h == corrupt)
	}
	require.Panics(t, func() { _, _ = nodeA.FinalizeBlock(stateFixBlock(fixHeight)) })

	// Node A restarts with the fix and continues.
	nodeA = newStateFixApp(t, dbA, fixes, false)
	require.Equal(t, corrupt, nodeA.LastBlockHeight())
	for h := fixHeight; h <= fixHeight+1; h++ {
		hashes[h] = runStateFixBlock(t, nodeA, h, false)
	}
	require.Equal(t, []int64{fixHeight}, ranAt)

	// The fix does not run again after its height is committed.
	nodeA = newStateFixApp(t, dbA, fixes, false)
	hashes[fixHeight+2] = runStateFixBlock(t, nodeA, fixHeight+2, false)
	require.Equal(t, []int64{fixHeight}, ranAt)

	// Node B syncs the same blocks from genesis with the fix.
	ranAt = nil
	nodeB := newStateFixApp(t, tmdb.NewMemDB(), fixes, true)
	for h := int64(1); h <= fixHeight+2; h++ {
		require.Equal(t, hashes[h], runStateFixBlock(t, nodeB, h, h == corrupt), "app hash at height %d", h)
	}
	require.Equal(t, []int64{fixHeight}, ranAt)
}

func TestStateFixErrorFailsBlock(t *testing.T) {
	fixErr := errors.New("fix failed")
	testApp := newStateFixApp(t, tmdb.NewMemDB(), []app.StateFix{{
		ChainID: util.ChainID,
		Height:  1,
		Name:    "failing",
		Apply:   func(sdk.Context, *app.App) error { return fixErr },
	}}, true)

	_, err := testApp.FinalizeBlock(stateFixBlock(1))
	require.ErrorIs(t, err, fixErr)

	req := stateFixBlock(1)
	req.Header = nil
	_, err = testApp.FinalizeBlock(req)
	require.ErrorContains(t, err, "block header is missing")
}

func TestStateFixesValid(t *testing.T) {
	seen := make(map[string]bool)
	for _, fix := range app.StateFixes() {
		require.NotEmpty(t, fix.ChainID, fix.Name)
		require.Positive(t, fix.Height, fix.Name)
		require.NotEmpty(t, fix.Name)
		require.NotNil(t, fix.Apply, fix.Name)
		key := fmt.Sprintf("%s/%d/%s", fix.ChainID, fix.Height, fix.Name)
		require.False(t, seen[key], "duplicate state fix %s", key)
		seen[key] = true
	}
}

// newStateFixApp opens an app on db with the given fixes. A fresh db gets the
// genesis state.
func newStateFixApp(t *testing.T, db tmdb.DB, fixes []app.StateFix, initChain bool) *app.App {
	t.Helper()
	testApp := app.New(log.NewNopLogger(), db, nil, 0, 0, util.EmptyAppOptions{}, baseapp.SetChainID(util.ChainID))
	testApp.SetStateFixes(fixes)
	if initChain {
		genesisState, _, _ := util.GenesisStateWithSingleValidator(testApp)
		testApp = util.InitialiseTestAppWithGenesis(testApp, app.DefaultConsensusParams(), genesisState)
	}
	return testApp
}

// runStateFixBlock executes and commits block h and returns its app hash. With
// dropMinter it deletes the minter before Commit, which halts the next block.
func runStateFixBlock(t *testing.T, testApp *app.App, h int64, dropMinter bool) []byte {
	t.Helper()
	_, err := testApp.FinalizeBlock(stateFixBlock(h))
	require.NoError(t, err)
	if dropMinter {
		mintStore := testApp.CommitMultiStore().GetCommitKVStore(testApp.GetKey(minttypes.StoreKey))
		mintStore.Delete(minttypes.KeyMinter)
	}
	_, err = testApp.Commit()
	require.NoError(t, err)
	return testApp.LastCommitID().Hash
}

func stateFixBlock(h int64) *abci.RequestFinalizeBlock {
	return &abci.RequestFinalizeBlock{
		Height: h,
		Time:   util.GenesisTime,
		Header: &cmtproto.Header{ChainID: util.ChainID, Height: h},
	}
}
