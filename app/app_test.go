package app_test

import (
	"encoding/json"
	"testing"
	"time"

	"cosmossdk.io/log"
	"github.com/celestiaorg/celestia-app/v7/app"
	"github.com/celestiaorg/celestia-app/v7/test/util"
	"github.com/celestiaorg/celestia-app/v7/test/util/testfactory"
	"github.com/celestiaorg/celestia-app/v7/test/util/testnode"
	minfeetypes "github.com/celestiaorg/celestia-app/v7/x/minfee/types"
	abci "github.com/cometbft/cometbft/abci/types"
	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"
	tmdb "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/baseapp"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
		{
			name: "should not panic on a genesis that does not contain an app version",
			request: abci.RequestInitChain{
				Time:    genesis.GenesisTime,
				ChainId: genesis.ChainID,
				ConsensusParams: &tmproto.ConsensusParams{
					Block:     &tmproto.BlockParams{},
					Evidence:  genesis.ConsensusParams.Evidence,
					Validator: genesis.ConsensusParams.Validator,
					Version:   &tmproto.VersionParams{}, // explicitly set to empty to remove app version.
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

func TestModuleAccountAddrs(t *testing.T) {
	t.Run("should contain all the module account addresses", func(t *testing.T) {
		testApp := getTestApp()
		got := testApp.ModuleAccountAddrs()

		want := map[string]bool{
			"celestia10d07y265gmmuvt4z0w9aw880jnsr700jtgz4v7": true,
			"celestia13d6j8m8tmeaz0t92a04azv5efmr8gxygtngtm9": true,
			"celestia17xpfvakm2amg962yls6f84z3kell8c5lpnjs3s": true,
			"celestia1fl48vsnmsdzcv85q5d2q4z5ajdha8yu3y3clr6": true,
			"celestia1jv65s3grqf6v6jl3dp4t6c9t9rk99cd8k44vnj": true,
			"celestia1m20fddqpmfuwcz2r9ckj6wd70p5e75t8y22wqj": true,
			"celestia1m3h30wlvsf8llruxtpukdvsy0km2kum8emkgad": true,
			"celestia1mqcszwafr476x3rud8qyufdegn7gvxh99rc2gk": true,
			"celestia1tygms3xhhs3yv487phx3dw4a95jn7t7ls3yw4w": true,
			"celestia1vlthgax23ca9syk7xgaz347xmf4nunefkz88ka": true,
			"celestia1yl6hdjhmkf37639730gffanpzndzdpmhl48edw": true,
		}
		assert.Equal(t, want, got)
	})
	t.Run("should be able to rederive the module account addresses from the module names", func(t *testing.T) {
		testApp := getTestApp()
		got := testApp.ModuleAccountAddrs()

		moduleNames := []string{
			"fee_collector",
			"distribution",
			"gov",
			"mint",
			"bonded_tokens_pool",
			"not_bonded_tokens_pool",
			"transfer",
			"interchainaccounts",
			"hyperlane",
			"warp",
			"forwarding",
		}
		for _, moduleName := range moduleNames {
			address := authtypes.NewModuleAddress(moduleName).String()
			assert.Contains(t, got, address)
		}
		assert.Equal(t, len(moduleNames), len(got))
	})
}

func TestBlockedAddresses(t *testing.T) {
	testApp := getTestApp()
	got := testApp.BlockedAddresses()

	t.Run("blocked addresses should not contain the gov module address", func(t *testing.T) {
		govAddress := authtypes.NewModuleAddress(govtypes.ModuleName).String()
		assert.NotContains(t, got, govAddress)
	})
	t.Run("blocked addresses should contain all the other module addresses", func(t *testing.T) {
		moduleNames := []string{
			"fee_collector",
			"distribution",
			"mint",
			"bonded_tokens_pool",
			"not_bonded_tokens_pool",
			"transfer",
			"interchainaccounts",
			"hyperlane",
			"warp",
			"forwarding",
		}
		for _, moduleName := range moduleNames {
			address := authtypes.NewModuleAddress(moduleName).String()
			assert.Contains(t, got, address)
		}
		assert.Equal(t, len(moduleNames), len(got))
	})
}

func TestNodeHome(t *testing.T) {
	// Test that NodeHome is accessible and non-empty
	assert.NotEmpty(t, app.NodeHome, "NodeHome should be set and non-empty")

	// Test that NodeHome contains the expected directory name
	assert.Contains(t, app.NodeHome, ".celestia-app", "NodeHome should contain .celestia-app directory")
}

// NoopWriter is a no-op implementation of a writer.
type NoopWriter struct{}

func (nw *NoopWriter) Write(p []byte) (n int, err error) {
	return len(p), nil
}

// NoopAppOptions is a no-op implementation of servertypes.AppOptions.
type NoopAppOptions struct{}

func (nao NoopAppOptions) Get(string) any {
	return nil
}

func getTestApp() *app.App {
	logger := log.NewNopLogger()
	db := tmdb.NewMemDB()
	traceStore := &NoopWriter{}
	timeoutCommit := time.Second
	appOptions := NoopAppOptions{}
	return app.New(logger, db, traceStore, timeoutCommit, appOptions)
}
