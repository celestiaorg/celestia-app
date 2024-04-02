package packetforward_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/app/encoding"
	v1 "github.com/celestiaorg/celestia-app/pkg/appconsts/v1"
	v2 "github.com/celestiaorg/celestia-app/pkg/appconsts/v2"
	"github.com/celestiaorg/celestia-app/test/util"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"github.com/cosmos/cosmos-sdk/x/params/types/proposal"
	packetforwardtypes "github.com/cosmos/ibc-apps/middleware/packet-forward-middleware/v6/router/types"
	"github.com/stretchr/testify/require"
	abci "github.com/tendermint/tendermint/abci/types"
	"github.com/tendermint/tendermint/libs/log"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	version "github.com/tendermint/tendermint/proto/tendermint/version"
	dbm "github.com/tendermint/tm-db"
)

// TestPacketForwardMiddlewareAgainstAppUpgrades verifies that the PFM module's params are overridden during an
// upgrade from v1 -> v2 and correctly set when upgrading to v2.
func TestPacketForwardMiddlewareAgainstAppUpgrades(t *testing.T) {
	testApp, _ := setupTestApp(t, 3)
	supportedVersions := []uint64{v1.Version, v2.Version}
	require.Equal(t, supportedVersions, testApp.SupportedVersions())

	ctx := testApp.NewContext(true, tmproto.Header{
		Version: version.Consensus{
			App: 1,
		},
	})
	testApp.BeginBlock(abci.RequestBeginBlock{Header: tmproto.Header{
		Height:  2,
		Version: version.Consensus{App: 1},
	}})

	// app version should not have changed yet
	require.EqualValues(t, 1, testApp.AppVersion())

	// PacketForwardMiddleware should not have been set yet
	gotBefore, err := testApp.ParamsKeeper.Params(ctx, &proposal.QueryParamsRequest{
		Subspace: packetforwardtypes.ModuleName,
		Key:      string(packetforwardtypes.KeyFeePercentage),
	})
	require.Equal(t, "", gotBefore.Param.Value)
	require.NoError(t, err)

	// now the app version changes
	respEndBlock := testApp.EndBlock(abci.RequestEndBlock{Height: 2})
	testApp.Commit()

	require.NotNil(t, respEndBlock.ConsensusParamUpdates.Version)
	require.EqualValues(t, 2, respEndBlock.ConsensusParamUpdates.Version.AppVersion)
	require.EqualValues(t, 2, testApp.AppVersion())

	// create a new context after endBlock
	newCtx := testApp.NewContext(true, tmproto.Header{
		Version: version.Consensus{
			App: 2,
		},
	})

	got, err := testApp.ParamsKeeper.Params(newCtx, &proposal.QueryParamsRequest{
		Subspace: packetforwardtypes.ModuleName,
		Key:      string(packetforwardtypes.KeyFeePercentage),
	})
	require.NoError(t, err)

	require.NoError(t, err)
	require.Equal(t, "0.000000000000000000", strings.Trim(got.Param.Value, "\""))
}

func setupTestApp(t *testing.T, upgradeHeight int64) (*app.App, keyring.Keyring) {
	t.Helper()

	db := dbm.NewMemDB()
	chainID := "test_chain"
	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	testApp := app.New(log.NewNopLogger(), db, nil, true, 0, encCfg, upgradeHeight, util.EmptyAppOptions{})
	genesisState, _, kr := util.GenesisStateWithSingleValidator(testApp, "account")
	stateBytes, err := json.MarshalIndent(genesisState, "", " ")
	require.NoError(t, err)
	infoResp := testApp.Info(abci.RequestInfo{})
	require.EqualValues(t, 0, infoResp.AppVersion)
	cp := app.DefaultInitialConsensusParams()
	abciParams := &abci.ConsensusParams{
		Block: &abci.BlockParams{
			MaxBytes: cp.Block.MaxBytes,
			MaxGas:   cp.Block.MaxGas,test/packetforward/packetforward_upgrade_test.go
		},
		Evidence:  &cp.Evidence,
		Validator: &cp.Validator,
		Version:   &cp.Version,
	}

	_ = testApp.InitChain(
		abci.RequestInitChain{
			Time:            time.Now(),
			Validators:      []abci.ValidatorUpdate{},
			ConsensusParams: abciParams,
			AppStateBytes:   stateBytes,
			ChainId:         chainID,
		},
	)

	// assert that the chain starts with version provided in genesis
	infoResp = testApp.Info(abci.RequestInfo{})
	require.EqualValues(t, app.DefaultInitialConsensusParams().Version.AppVersion, infoResp.AppVersion)

	_ = testApp.Commit()
	return testApp, kr
}
