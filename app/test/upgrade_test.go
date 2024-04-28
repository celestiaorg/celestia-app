package app_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	app "github.com/celestiaorg/celestia-app/v2/app"
	"github.com/celestiaorg/celestia-app/v2/app/encoding"
	v1 "github.com/celestiaorg/celestia-app/v2/pkg/appconsts/v1"
	v2 "github.com/celestiaorg/celestia-app/v2/pkg/appconsts/v2"
	"github.com/celestiaorg/celestia-app/v2/test/util"
	blobstreamtypes "github.com/celestiaorg/celestia-app/v2/x/blobstream/types"
	"github.com/celestiaorg/celestia-app/v2/x/minfee"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/params/types/proposal"
	packetforwardtypes "github.com/cosmos/ibc-apps/middleware/packet-forward-middleware/v6/packetforward/types"
	icahosttypes "github.com/cosmos/ibc-go/v6/modules/apps/27-interchain-accounts/host/types"
	"github.com/stretchr/testify/require"
	abci "github.com/tendermint/tendermint/abci/types"
	"github.com/tendermint/tendermint/libs/log"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	tmversion "github.com/tendermint/tendermint/proto/tendermint/version"
	dbm "github.com/tendermint/tm-db"
)

// TestAppUpgrades verifies that the all module's params are overridden during an
// upgrade from v1 -> v2 and the app version changes correctly.
func TestAppUpgrades(t *testing.T) {
	GlobalMinGasPriceDec, err := sdk.NewDecFromStr(fmt.Sprintf("%f", v2.GlobalMinGasPrice))
	require.NoError(t, err)

	tests := []struct {
		module        string
		subspace      string
		key           string
		expectedValue string
	}{
		{
			module:        "MinFee",
			subspace:      minfee.ModuleName,
			key:           string(minfee.KeyGlobalMinGasPrice),
			expectedValue: GlobalMinGasPriceDec.String(),
		},
		{
			module:        "ICA",
			subspace:      icahosttypes.SubModuleName,
			key:           string(icahosttypes.KeyHostEnabled),
			expectedValue: "true",
		},
		{
			module:        "PFM",
			subspace:      packetforwardtypes.ModuleName,
			key:           string(packetforwardtypes.KeyFeePercentage),
			expectedValue: "0.000000000000000000",
		},
	}
	for _, tt := range tests {
		t.Run(tt.module, func(t *testing.T) {
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
			// app version should not have changed yet
			require.EqualValues(t, 1, testApp.AppVersion())

			// Query the module params
			gotBefore, err := testApp.ParamsKeeper.Params(ctx, &proposal.QueryParamsRequest{
				Subspace: tt.subspace,
				Key:      tt.key,
			})
			require.NoError(t, err)
			require.Equal(t, "", gotBefore.Param.Value)

			// Upgrade from v1 -> v2
			testApp.EndBlock(abci.RequestEndBlock{Height: 2})
			testApp.Commit()
			require.EqualValues(t, 2, testApp.AppVersion())

			newCtx := testApp.NewContext(true, tmproto.Header{Version: tmversion.Consensus{App: 2}})
			got, err := testApp.ParamsKeeper.Params(newCtx, &proposal.QueryParamsRequest{
				Subspace: tt.subspace,
				Key:      tt.key,
			})
			require.NoError(t, err)
			require.Equal(t, tt.expectedValue, strings.Trim(got.Param.Value, "\""))
		})
	}
}

// TestBlobstreamRemovedInV2 verifies that the blobstream params exist in v1 and
// do not exist in v2.
func TestBlobstreamRemovedInV2(t *testing.T) {
	testApp, _ := SetupTestAppWithUpgradeHeight(t, 3)
	ctx := testApp.NewContext(true, tmproto.Header{})

	require.EqualValues(t, 1, testApp.AppVersion())
	got, err := testApp.ParamsKeeper.Params(ctx, &proposal.QueryParamsRequest{
		Subspace: blobstreamtypes.ModuleName,
		Key:      string(blobstreamtypes.ParamsStoreKeyDataCommitmentWindow),
	})
	require.NoError(t, err)
	require.Equal(t, "\"400\"", got.Param.Value)

	upgradeFromV1ToV2(t, testApp)

	require.EqualValues(t, 2, testApp.AppVersion())
	_, err = testApp.ParamsKeeper.Params(ctx, &proposal.QueryParamsRequest{
		Subspace: blobstreamtypes.ModuleName,
		Key:      string(blobstreamtypes.ParamsStoreKeyDataCommitmentWindow),
	})
	require.Error(t, err)
}

func SetupTestAppWithUpgradeHeight(t *testing.T, upgradeHeight int64) (*app.App, keyring.Keyring) {
	t.Helper()

	db := dbm.NewMemDB()
	chainID := "test_chain"
	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	testApp := app.New(log.NewNopLogger(), db, nil, 0, encCfg, upgradeHeight, util.EmptyAppOptions{})
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

	supportedVersions := []uint64{v1.Version, v2.Version}
	require.Equal(t, supportedVersions, testApp.SupportedVersions())

	_ = testApp.Commit()
	return testApp, kr
}

func upgradeFromV1ToV2(t *testing.T, testApp *app.App) {
	t.Helper()
	testApp.BeginBlock(abci.RequestBeginBlock{Header: tmproto.Header{
		Height:  2,
		Version: tmversion.Consensus{App: 1},
	}})
	testApp.EndBlock(abci.RequestEndBlock{Height: 2})
	testApp.Commit()
	require.EqualValues(t, 2, testApp.AppVersion())
}
