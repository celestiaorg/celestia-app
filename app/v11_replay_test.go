package app_test

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"cosmossdk.io/core/header"
	"cosmossdk.io/log"
	"github.com/celestiaorg/celestia-app/v10/app"
	"github.com/celestiaorg/celestia-app/v10/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v10/test/util"
	fibretypes "github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	signaltypes "github.com/celestiaorg/celestia-app/v10/x/signal/types"
	abci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	cmtversion "github.com/cometbft/cometbft/proto/tendermint/version"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/baseapp"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

type replayOptions struct{ home string }

func (o replayOptions) Get(key string) any {
	if key == "home" {
		return o.home
	}
	return nil
}

func replayApp(t *testing.T, home string) *app.App {
	t.Helper()
	db, err := dbm.NewDB("application", dbm.GoLevelDBBackend, home)
	require.NoError(t, err)
	return app.New(log.NewNopLogger(), db, nil, 0, 0, replayOptions{home}, baseapp.SetChainID(appconsts.TestChainID))
}

func initializeV10Replay(t *testing.T, a *app.App) {
	t.Helper()
	genesis, _, _ := util.GenesisStateWithSingleValidator(a)
	fibreGenesis := fibretypes.DefaultGenesis()
	fibreGenesis.Params = fibretypes.DefaultParamsForVersion(10)
	genesis[fibretypes.ModuleName] = a.AppCodec().MustMarshalJSON(fibreGenesis)
	raw, err := json.Marshal(genesis)
	require.NoError(t, err)
	cp := app.DefaultConsensusParams()
	cp.Version.App = 10
	_, err = a.InitChain(&abci.RequestInitChain{Time: util.GenesisTime, ChainId: appconsts.TestChainID, ConsensusParams: cp, AppStateBytes: raw})
	require.NoError(t, err)
}

func replayBlock(t *testing.T, a *app.App, height int64) {
	t.Helper()
	_, err := a.FinalizeBlock(&abci.RequestFinalizeBlock{Height: height, Time: util.GenesisTime.Add(time.Duration(height) * time.Second), Hash: a.LastCommitID().Hash})
	require.NoError(t, err)
	_, err = a.Commit()
	require.NoError(t, err)
}

func TestV10ReplayAndRestart(t *testing.T) {
	// Recorded using the unchanged fa5b523b7 v10 application on this deterministic fixture.
	expected := []string{
		"D65BD2B3E3956D009D04B257FF7C52775BBAF898F7E90FF047DFCE96C2D87E34",
		"6CD1EBCD838C6B3EA42FF6A9FA3D4B0E22850203384707A1CD35BF31120FD70A",
		"6C3A81D784B5F8225A2E0EE818AA79535C53684D582B2E816CDB3671F6D3C5C3",
		"09C660BFCC416FAC1AED8080A7EAF3431E55CFFDF94EB2B84AB4F0FE42961302",
	}
	home := t.TempDir()
	a := replayApp(t, home)
	initializeV10Replay(t, a)
	for h := int64(1); h <= 3; h++ {
		replayBlock(t, a, h)
		require.Equal(t, expected[h-1], fmt.Sprintf("%X", a.LastCommitID().Hash))
	}
	hash := a.LastCommitID().Hash
	require.NoError(t, a.Close())
	a = replayApp(t, home)
	defer func() { require.NoError(t, a.Close()) }()
	require.Equal(t, int64(3), a.LastBlockHeight())
	require.Equal(t, hash, a.LastCommitID().Hash)
	ctx := a.NewUncachedContext(false, cmtproto.Header{})
	version, err := a.AppVersion(ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(10), version)
	replayBlock(t, a, 4)
	require.Equal(t, expected[3], fmt.Sprintf("%X", a.LastCommitID().Hash))
}

func TestV11SignalActivationAndRestart(t *testing.T) {
	home := t.TempDir()
	a := replayApp(t, home)
	initializeV10Replay(t, a)
	replayBlock(t, a, 1)
	ctx := a.NewUncachedContext(false, cmtproto.Header{Height: 1, ChainID: appconsts.TestChainID, Version: cmtversion.Consensus{App: 10}}).WithHeaderInfo(header.Info{Height: 1, ChainID: appconsts.TestChainID})
	vals, err := a.StakingKeeper.GetAllValidators(ctx)
	require.NoError(t, err)
	valAddr, err := sdk.ValAddressFromBech32(vals[0].OperatorAddress)
	require.NoError(t, err)
	_, err = a.SignalKeeper.SignalVersion(ctx, &signaltypes.MsgSignalVersion{ValidatorAddress: valAddr.String(), Version: 11})
	require.NoError(t, err)
	_, err = a.SignalKeeper.TryUpgrade(ctx, &signaltypes.MsgTryUpgrade{Signer: valAddr.String()})
	require.NoError(t, err)
	// The existing test-chain delay is three blocks; Corto's delay is unchanged.
	for h := int64(2); h <= 4; h++ {
		before, err := a.AppVersion(a.NewUncachedContext(false, cmtproto.Header{}))
		require.NoError(t, err)
		require.Equal(t, uint64(10), before)
		replayBlock(t, a, h)
	}
	require.NoError(t, a.Close())
	// Restart immediately before the v11 migration: upgrade-info exists, stores already exist.
	a = replayApp(t, home)
	defer func() { require.NoError(t, a.Close()) }()
	ctx = a.NewUncachedContext(false, cmtproto.Header{})
	version, err := a.AppVersion(ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(11), version)
	plan, err := a.UpgradeKeeper.GetUpgradePlan(ctx)
	require.NoError(t, err)
	require.Equal(t, "v11", plan.Name)
	require.Equal(t, int64(5), plan.Height)
	replayBlock(t, a, 5)
	done, err := a.UpgradeKeeper.GetDoneHeight(a.NewUncachedContext(false, cmtproto.Header{}), "v11")
	require.NoError(t, err)
	require.Equal(t, int64(5), done)
	replayBlock(t, a, 6)
}
