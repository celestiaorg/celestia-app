package app_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v3/app"
	"github.com/celestiaorg/celestia-app/v3/app/encoding"
	"github.com/celestiaorg/celestia-app/v3/test/util"
	"github.com/celestiaorg/celestia-app/v3/test/util/testnode"
	"github.com/celestiaorg/celestia-app/v3/x/minfee"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/snapshots"
	snapshottypes "github.com/cosmos/cosmos-sdk/snapshots/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	abci "github.com/tendermint/tendermint/abci/types"
	"github.com/tendermint/tendermint/libs/log"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	tmdb "github.com/tendermint/tm-db"
)

func TestNew(t *testing.T) {
	logger := log.NewNopLogger()
	db := tmdb.NewMemDB()
	traceStore := &NoopWriter{}
	invCheckPeriod := uint(1)
	encodingConfig := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	upgradeHeight := int64(0)
	timeoutCommit := time.Second
	appOptions := NoopAppOptions{}

	got := app.New(logger, db, traceStore, invCheckPeriod, encodingConfig, upgradeHeight, timeoutCommit, appOptions)

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
		// hooks have been set by verifying the a subsequent call to SetHooks
		// will panic.
		assert.Panics(t, func() { got.StakingKeeper.SetHooks(nil) })
	})
	t.Run("should not have sealed the baseapp", func(t *testing.T) {
		assert.False(t, got.IsSealed())
	})
	t.Run("should have set the minfee key table", func(t *testing.T) {
		subspace := got.GetSubspace(minfee.ModuleName)
		hasKeyTable := subspace.HasKeyTable()
		assert.True(t, hasKeyTable)
	})
}

func TestInitChain(t *testing.T) {
	logger := log.NewNopLogger()
	db := tmdb.NewMemDB()
	traceStore := &NoopWriter{}
	invCheckPeriod := uint(1)
	encodingConfig := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	upgradeHeight := int64(0)
	timeoutCommit := time.Second
	appOptions := NoopAppOptions{}
	testApp := app.New(logger, db, traceStore, invCheckPeriod, encodingConfig, upgradeHeight, timeoutCommit, appOptions)
	genesisState, _, _ := util.GenesisStateWithSingleValidator(testApp, "account")
	appStateBytes, err := json.MarshalIndent(genesisState, "", " ")
	require.NoError(t, err)
	genesis := testnode.DefaultConfig().Genesis

	type testCase struct {
		name      string
		request   abci.RequestInitChain
		wantPanic bool
	}
	testCases := []testCase{
		{
			name:      "should panic if consensus params not set",
			request:   abci.RequestInitChain{},
			wantPanic: true,
		},
		{
			name: "should not panic on a genesis that does not contain an app version",
			request: abci.RequestInitChain{
				Time:    genesis.GenesisTime,
				ChainId: genesis.ChainID,
				ConsensusParams: &abci.ConsensusParams{
					Block:     &abci.BlockParams{},
					Evidence:  &genesis.ConsensusParams.Evidence,
					Validator: &genesis.ConsensusParams.Validator,
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
			application := app.New(logger, db, traceStore, invCheckPeriod, encodingConfig, upgradeHeight, timeoutCommit, appOptions)
			if tc.wantPanic {
				assert.Panics(t, func() { application.InitChain(tc.request) })
			} else {
				assert.NotPanics(t, func() { application.InitChain(tc.request) })
			}
		})
	}
}

func TestOfferSnapshot(t *testing.T) {
	t.Run("should ACCEPT a snapshot with app version 0", func(t *testing.T) {
		// Snapshots taken before the app version field was introduced to RequestOfferSnapshot should still be accepted.
		app := createTestApp(t)
		request := createRequest()
		want := abci.ResponseOfferSnapshot{Result: abci.ResponseOfferSnapshot_ACCEPT}
		got := app.OfferSnapshot(request)
		assert.Equal(t, want, got)
	})
	t.Run("should ACCEPT a snapshot with app version 1", func(t *testing.T) {
		app := createTestApp(t)
		request := createRequest()
		request.AppVersion = 1
		want := abci.ResponseOfferSnapshot{Result: abci.ResponseOfferSnapshot_ACCEPT}
		got := app.OfferSnapshot(request)
		assert.Equal(t, want, got)
	})
	t.Run("should ACCEPT a snapshot with app version 2", func(t *testing.T) {
		app := createTestApp(t)
		request := createRequest()
		request.AppVersion = 2
		want := abci.ResponseOfferSnapshot{Result: abci.ResponseOfferSnapshot_ACCEPT}
		got := app.OfferSnapshot(request)
		assert.Equal(t, want, got)
	})
	t.Run("should ACCEPT a snapshot with app version 3", func(t *testing.T) {
		app := createTestApp(t)
		request := createRequest()
		request.AppVersion = 3
		want := abci.ResponseOfferSnapshot{Result: abci.ResponseOfferSnapshot_ACCEPT}
		got := app.OfferSnapshot(request)
		assert.Equal(t, want, got)
	})
	t.Run("should REJECT a snapshot with unsupported app version", func(t *testing.T) {
		app := createTestApp(t)
		request := createRequest()
		request.AppVersion = 4 // unsupported app version
		want := abci.ResponseOfferSnapshot{Result: abci.ResponseOfferSnapshot_REJECT}
		got := app.OfferSnapshot(request)
		assert.Equal(t, want, got)
	})
}

func createTestApp(t *testing.T) *app.App {
	db := tmdb.NewMemDB()
	config := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	upgradeHeight := int64(3)
	timeoutCommit := time.Second
	snapshotDir := filepath.Join(t.TempDir(), "data", "snapshots")
	t.Cleanup(func() {
		err := os.RemoveAll(snapshotDir)
		require.NoError(t, err)
	})
	snapshotDB, err := tmdb.NewDB("metadata", tmdb.GoLevelDBBackend, snapshotDir)
	t.Cleanup(func() {
		err := snapshotDB.Close()
		require.NoError(t, err)
	})
	require.NoError(t, err)
	snapshotStore, err := snapshots.NewStore(snapshotDB, snapshotDir)
	require.NoError(t, err)
	baseAppOption := baseapp.SetSnapshot(snapshotStore, snapshottypes.NewSnapshotOptions(10, 10))
	testApp := app.New(log.NewNopLogger(), db, nil, 0, config, upgradeHeight, timeoutCommit, util.EmptyAppOptions{}, baseAppOption)
	require.NoError(t, err)
	response := testApp.Info(abci.RequestInfo{})
	require.Equal(t, uint64(0), response.AppVersion)
	return testApp
}

func createRequest() abci.RequestOfferSnapshot {
	return abci.RequestOfferSnapshot{
		// Snapshot was created by logging the contents of OfferSnapshot on a
		// node that was syncing via state sync.
		Snapshot: &abci.Snapshot{
			Height:   0x1b07ec,
			Format:   0x2,
			Chunks:   0x1,
			Hash:     []uint8{0xaf, 0xa5, 0xe, 0x16, 0x45, 0x4, 0x2e, 0x45, 0xd3, 0x49, 0xdf, 0x83, 0x2a, 0x57, 0x9d, 0x64, 0xc8, 0xad, 0xa5, 0xb, 0x65, 0x1b, 0x46, 0xd6, 0xc3, 0x85, 0x6, 0x51, 0xd7, 0x45, 0x8e, 0xb8},
			Metadata: []uint8{0xa, 0x20, 0xaf, 0xa5, 0xe, 0x16, 0x45, 0x4, 0x2e, 0x45, 0xd3, 0x49, 0xdf, 0x83, 0x2a, 0x57, 0x9d, 0x64, 0xc8, 0xad, 0xa5, 0xb, 0x65, 0x1b, 0x46, 0xd6, 0xc3, 0x85, 0x6, 0x51, 0xd7, 0x45, 0x8e, 0xb8},
		},
		AppHash:    []byte("apphash"),
		AppVersion: 0, // unit tests will override this
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
