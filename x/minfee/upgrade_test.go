package minfee_test

import (
	// "context"
	"encoding/json"
	// "strconv"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/app/encoding"
	"github.com/celestiaorg/celestia-app/x/minfee"

	v1 "github.com/celestiaorg/celestia-app/pkg/appconsts/v1"
	v2 "github.com/celestiaorg/celestia-app/pkg/appconsts/v2"
	"github.com/celestiaorg/celestia-app/test/util"

	// "github.com/celestiaorg/celestia-app/x/minfee"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"

	sdk "github.com/cosmos/cosmos-sdk/types"

	// params "github.com/cosmos/cosmos-sdk/x/params/types"
	"github.com/cosmos/cosmos-sdk/x/params/types/proposal"
	"github.com/stretchr/testify/require"
	abci "github.com/tendermint/tendermint/abci/types"
	"github.com/tendermint/tendermint/libs/log"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	tmversion "github.com/tendermint/tendermint/proto/tendermint/version"
	dbm "github.com/tendermint/tm-db"

	version "github.com/tendermint/tendermint/proto/tendermint/version"
)

func TestUpgradeAppVersion(t *testing.T) {
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
		Version: tmversion.Consensus{App: 1},
	}})

	// app version should not have changed yet

	// _, err := testApp.ParamsKeeper.Params(ctx, &proposal.QueryParamsRequest{
	// 	Subspace: minfee.ModuleName,
	// 	Key:      string(minfee.KeyGlobalMinGasPrice),
	// })
	require.EqualValues(t, 1, testApp.AppVersion(ctx))

	// fmt.Println(response, "RES FROM INITGENESIS")
	// testApp.Commit()

	// now the app version changes
	respEndBlock := testApp.EndBlock(abci.RequestEndBlock{Height: 2})
	require.NotNil(t, respEndBlock.ConsensusParamUpdates.Version)
	require.EqualValues(t, 2, respEndBlock.ConsensusParamUpdates.Version.AppVersion)
	require.EqualValues(t, 2, testApp.AppVersion(ctx))
	testApp.Commit()

	got, err := testApp.ParamsKeeper.Params(ctx, &proposal.QueryParamsRequest{
		Subspace: minfee.ModuleName,
		Key:      string(minfee.KeyGlobalMinGasPrice),
	})
	require.NoError(t, err)

	want, err := sdk.NewDecFromStr(fmt.Sprintf("%f", v2.GlobalMinGasPrice))
	require.Equal(t, want.String(), strings.Trim(got.Param.Value, "\""))
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

	cp := app.DefaultConsensusParams()
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
	require.EqualValues(t, app.DefaultConsensusParams().Version.AppVersion, infoResp.AppVersion)

	_ = testApp.Commit()
	return testApp, kr
}
