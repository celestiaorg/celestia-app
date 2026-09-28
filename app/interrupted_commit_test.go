package app_test

import (
	"testing"

	"cosmossdk.io/log"
	"github.com/celestiaorg/celestia-app/v10/app"
	"github.com/celestiaorg/celestia-app/v10/test/util"
	abci "github.com/cometbft/cometbft/abci/types"
	tmdb "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/baseapp"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/cosmos/iavl"
	"github.com/stretchr/testify/require"
)

// TestRestartAfterInterruptedCommit checks that a node killed during Commit
// can replay the block after a restart instead of panicking.
func TestRestartAfterInterruptedCommit(t *testing.T) {
	db := tmdb.NewMemDB()
	newApp := func() *app.App {
		return app.New(log.NewNopLogger(), db, nil, 0, 0, util.EmptyAppOptions{}, baseapp.SetChainID(util.ChainID))
	}

	testApp := newApp()
	genesisState, _, _ := util.GenesisStateWithSingleValidator(testApp)
	testApp = util.InitialiseTestAppWithGenesis(testApp, app.DefaultConsensusParams(), genesisState)
	finalizeAndCommit(t, testApp)
	committed := testApp.LastBlockHeight()
	torn := committed + 1

	// Simulate a crash during Commit: the bank IAVL store writes version torn
	// but not its root node, and the multistore stays at version committed.
	_, err := testApp.FinalizeBlock(&abci.RequestFinalizeBlock{Height: torn, Time: util.GenesisTime})
	require.NoError(t, err)
	bankStore := testApp.CommitMultiStore().GetCommitKVStore(testApp.GetKey(banktypes.StoreKey))
	bankStore.Set([]byte("torn"), []byte("torn"))
	require.Equal(t, torn, bankStore.Commit().Version)
	bankDB := tmdb.NewPrefixDB(db, []byte("s/k:"+banktypes.StoreKey+"/"))
	require.NoError(t, bankDB.Delete(append([]byte{'s'}, iavl.GetRootKey(torn)...)))

	restarted := newApp()
	require.Equal(t, committed, restarted.LastBlockHeight())
	restartedBankStore := restarted.CommitMultiStore().GetCommitKVStore(restarted.GetKey(banktypes.StoreKey))
	require.Nil(t, restartedBankStore.Get([]byte("torn")))
	require.NotPanics(t, func() { finalizeAndCommit(t, restarted) })
	require.Equal(t, torn, restarted.LastBlockHeight())
	require.Nil(t, restartedBankStore.Get([]byte("torn")))
}

func finalizeAndCommit(t *testing.T, testApp *app.App) {
	t.Helper()
	_, err := testApp.FinalizeBlock(&abci.RequestFinalizeBlock{Height: testApp.LastBlockHeight() + 1, Time: util.GenesisTime})
	require.NoError(t, err)
	_, err = testApp.Commit()
	require.NoError(t, err)
}
