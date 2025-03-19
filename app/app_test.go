package app_test

import (
	"encoding/json"
	"testing"
	"time"

	"cosmossdk.io/log"
	abci "github.com/cometbft/cometbft/abci/types"
	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"
	tmdb "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/celestiaorg/celestia-app/v4/app"
	"github.com/celestiaorg/celestia-app/v4/test/util"
	"github.com/celestiaorg/celestia-app/v4/test/util/testfactory"
	"github.com/celestiaorg/celestia-app/v4/test/util/testnode"
	minfeetypes "github.com/celestiaorg/celestia-app/v4/x/minfee/types"
)

func TestNew(t *testing.T) {
	logger := log.NewNopLogger()
	db := tmdb.NewMemDB()
	traceStore := &NoopWriter{}
	timeoutCommit := time.Second
	appOptions := NoopAppOptions{}

	got := app.New(logger, db, traceStore, timeoutCommit, appOptions)

	t.Run("initializes ICAHostKeeper", func(t *testing.T) {
		assert.NotNil(t, got.ICAHostKeeper)
	})
	t.Run("initializes ScopedICAHostKeeper", func(t *testing.T) {
		assert.NotNil(t, got.ScopedICAHostKeeper)
	})
	t.Run("initializes StakingKeeper", func(t *testing.T) {
		assert.NotNil(t, got.StakingKeeper)
	})
	t.Run("should have set StakingKeeper hooks", func(t *testing.T) {
		// StakingKeeper doesn't expose a GetHooks method so this checks if
		// hooks have been set by verifying the subsequent call to SetHooks
		// will panic.
		assert.Panics(t, func() { got.StakingKeeper.SetHooks(nil) })
	})
	// TODO: baseapp is now sealed in baseapp.Init() called by LoadLatestVersion in app.go
	// t.Run("should not have sealed the baseapp", func(t *testing.T) {
	// 	assert.False(t, got.IsSealed())
	// })
	t.Run("should have set the minfee key table", func(t *testing.T) {
		subspace := got.GetSubspace(minfeetypes.ModuleName)
		hasKeyTable := subspace.HasKeyTable()
		assert.True(t, hasKeyTable)
	})
}

func TestInitChain(t *testing.T) {
	logger := log.NewNopLogger()
	db := tmdb.NewMemDB()
	traceStore := &NoopWriter{}
	timeoutCommit := time.Second
	appOptions := NoopAppOptions{}
	testApp := app.New(logger, db, traceStore, timeoutCommit, appOptions, baseapp.SetChainID(testfactory.ChainID))
	genesisState, _, _ := util.GenesisStateWithSingleValidator(testApp, "account")
	appStateBytes, err := json.MarshalIndent(genesisState, "", " ")
	require.NoError(t, err)
	genesis := testnode.DefaultConfig().Genesis.WithChainID(testApp.ChainID())

	type testCase struct {
		name      string
		request   abci.RequestInitChain
		wantPanic bool
	}
	testCases := []testCase{
		// TODO: don't think we want this?
		// {
		// 	name:      "should panic if consensus params not set",
		// 	request:   abci.RequestInitChain{},
		// 	wantPanic: true,
		// },
		{
			name: "should not panic on a genesis that does not contain an app version",
			request: abci.RequestInitChain{
				Time:    genesis.GenesisTime,
				ChainId: genesis.ChainID,
				ConsensusParams: &tmproto.ConsensusParams{
					Block:     &tmproto.BlockParams{},
					Evidence:  genesis.ConsensusParams.Evidence,
					Validator: genesis.ConsensusParams.Validator,
					Version:   &tmproto.VersionParams{}, // explicitly set to empty to remove app version.,
				},
				AppStateBytes: appStateBytes,
				InitialHeight: 0,
			},
			wantPanic: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			application := app.New(logger, db, traceStore, timeoutCommit, appOptions, baseapp.SetChainID(testfactory.ChainID))
			if tc.wantPanic {
				_, err := application.InitChain(&tc.request)
				assert.Error(t, err)
			} else {
				_, err := application.InitChain(&tc.request)
				assert.NoError(t, err)
			}
		})
	}
}

// NoopWriter is a no-op implementation of a writer.
type NoopWriter struct{}

func (nw *NoopWriter) Write(p []byte) (n int, err error) {
	return len(p), nil
}

// NoopAppOptions is a no-op implementation of servertypes.AppOptions.
type NoopAppOptions struct{}

func (nao NoopAppOptions) Get(string) interface{} {
	return nil
}
