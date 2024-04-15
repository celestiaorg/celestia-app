package app_test

import (
	"testing"

	"fmt"
	app "github.com/celestiaorg/celestia-app/v2/app"
	"github.com/celestiaorg/celestia-app/v2/app/encoding"
	v1 "github.com/celestiaorg/celestia-app/v2/pkg/appconsts/v1"
	v2 "github.com/celestiaorg/celestia-app/v2/pkg/appconsts/v2"
	"github.com/celestiaorg/celestia-app/v2/test/util"
	"github.com/celestiaorg/celestia-app/v2/x/minfee"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/params/types/proposal"
	icahosttypes "github.com/cosmos/ibc-go/v6/modules/apps/27-interchain-accounts/host/types"
	"github.com/stretchr/testify/require"
	abci "github.com/tendermint/tendermint/abci/types"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	tmversion "github.com/tendermint/tendermint/proto/tendermint/version"
	dbm "github.com/tendermint/tm-db"
	"encoding/json"

	"github.com/tendermint/tendermint/libs/log"
	"strings"
	"time"
)

func TestUpgradeAppVersion(t *testing.T) {
	testApp, _ := SetupTestAppWithUpgradeHeight(t, 3)

	supportedVersions := []uint64{v1.Version, v2.Version}

	require.Equal(t, supportedVersions, testApp.SupportedVersions())

	testApp.BeginBlock(abci.RequestBeginBlock{Header: tmproto.Header{
		Height:  2,
		Version: tmversion.Consensus{App: 1},
	}})
	// app version should not have changed yet
	require.EqualValues(t, 1, testApp.AppVersion())
	respEndBlock := testApp.EndBlock(abci.RequestEndBlock{Height: 2})
	// now the app version changes
	require.NotNil(t, respEndBlock.ConsensusParamUpdates.Version)
	require.EqualValues(t, 2, respEndBlock.ConsensusParamUpdates.Version.AppVersion)
	require.EqualValues(t, 2, testApp.AppVersion())
}

func TestMinFeeDuringVersionUpgrades(t *testing.T) {
	testApp, _ := SetupTestAppWithUpgradeHeight(t, 3)

	supportedVersions := []uint64{v1.Version, v2.Version}

	require.Equal(t, supportedVersions, testApp.SupportedVersions())

	ctx := testApp.NewContext(true, tmproto.Header{
		Version: tmversion.Consensus{
			App: 1,
		},
	})
	testApp.BeginBlock(abci.RequestBeginBlock{Header: tmproto.Header{
		Height:  2,
		Version: tmversion.Consensus{App: 1},
	}})

	// app version should not have changed yet
	require.EqualValues(t, 1, testApp.AppVersion())

	// global min gas price should not have been set yet
	gotBefore, err := testApp.ParamsKeeper.Params(ctx, &proposal.QueryParamsRequest{
		Subspace: minfee.ModuleName,
		Key:      string(minfee.KeyGlobalMinGasPrice),
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
		Version: tmversion.Consensus{
			App: 2,
		},
	})

	// global min gas price should be set
	got, err := testApp.ParamsKeeper.Params(newCtx, &proposal.QueryParamsRequest{
		Subspace: minfee.ModuleName,
		Key:      string(minfee.KeyGlobalMinGasPrice),
	})
	require.NoError(t, err)

	want, err := sdk.NewDecFromStr(fmt.Sprintf("%f", v2.GlobalMinGasPrice))
	require.NoError(t, err)
	require.Equal(t, want.String(), strings.Trim(got.Param.Value, "\""))
}

func TestICADuringVersionUpgrades(t *testing.T) {
	testApp, _ := SetupTestAppWithUpgradeHeight(t, 3)
	ctx := testApp.NewContext(true, tmproto.Header{
		Version: tmversion.Consensus{
			App: 1,
		},
	})
	testApp.BeginBlock(abci.RequestBeginBlock{Header: tmproto.Header{
		Height:  2,
		Version: tmversion.Consensus{App: 1},
	}})
	require.EqualValues(t, 1, testApp.AppVersion())

	// Query the ICA host module params
	gotBefore, err := testApp.ParamsKeeper.Params(ctx, &proposal.QueryParamsRequest{
		Subspace: icahosttypes.SubModuleName,
		Key:      string(icahosttypes.KeyHostEnabled),
	})
	require.NoError(t, err)
	require.Equal(t, "", gotBefore.Param.Value)

	// Upgrade from v1 -> v2
	testApp.EndBlock(abci.RequestEndBlock{Height: 2})
	testApp.Commit()
	require.EqualValues(t, 2, testApp.AppVersion())

	newCtx := testApp.NewContext(true, tmproto.Header{Version: tmversion.Consensus{App: 2}})
	got, err := testApp.ParamsKeeper.Params(newCtx, &proposal.QueryParamsRequest{
		Subspace: icahosttypes.SubModuleName,
		Key:      string(icahosttypes.KeyHostEnabled),
	})
	require.NoError(t, err)
	require.Equal(t, "true", got.Param.Value)
}

func SetupTestAppWithUpgradeHeight(t *testing.T, upgradeHeight int64) (*app.App, keyring.Keyring) {
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
			MaxGas:   cp.Block.MaxGas,
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
