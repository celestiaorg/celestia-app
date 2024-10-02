package app_test

import (
	"fmt"
	"strings"
	"testing"

	app "github.com/celestiaorg/celestia-app/v3/app"
	"github.com/celestiaorg/celestia-app/v3/app/encoding"
	"github.com/celestiaorg/celestia-app/v3/pkg/appconsts"
	v1 "github.com/celestiaorg/celestia-app/v3/pkg/appconsts/v1"
	v2 "github.com/celestiaorg/celestia-app/v3/pkg/appconsts/v2"
	v3 "github.com/celestiaorg/celestia-app/v3/pkg/appconsts/v3"
	"github.com/celestiaorg/celestia-app/v3/pkg/user"
	"github.com/celestiaorg/celestia-app/v3/test/util"
	"github.com/celestiaorg/celestia-app/v3/test/util/genesis"
	"github.com/celestiaorg/celestia-app/v3/test/util/testnode"
	blobstreamtypes "github.com/celestiaorg/celestia-app/v3/x/blobstream/types"
	"github.com/celestiaorg/celestia-app/v3/x/minfee"
	signaltypes "github.com/celestiaorg/celestia-app/v3/x/signal/types"
	"github.com/celestiaorg/go-square/v2/share"
	"github.com/celestiaorg/go-square/v2/tx"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
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

func TestAppUpgradeV3(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping TestAppUpgradeV3 in short mode")
	}
	testApp, genesis := SetupTestAppWithUpgradeHeight(t, 3)
	upgradeFromV1ToV2(t, testApp)

	ctx := testApp.NewContext(true, tmproto.Header{})
	validators := testApp.StakingKeeper.GetAllValidators(ctx)
	valAddr, err := sdk.ValAddressFromBech32(validators[0].OperatorAddress)
	require.NoError(t, err)
	record, err := genesis.Keyring().Key(testnode.DefaultValidatorAccountName)
	require.NoError(t, err)
	accAddr, err := record.GetAddress()
	require.NoError(t, err)
	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	resp, err := testApp.AccountKeeper.Account(ctx, &authtypes.QueryAccountRequest{
		Address: accAddr.String(),
	})
	require.NoError(t, err)
	var account authtypes.AccountI
	err = encCfg.InterfaceRegistry.UnpackAny(resp.Account, &account)
	require.NoError(t, err)

	signer, err := user.NewSigner(
		genesis.Keyring(), encCfg.TxConfig, testApp.GetChainID(), v3.Version,
		user.NewAccount(testnode.DefaultValidatorAccountName, account.GetAccountNumber(), account.GetSequence()),
	)
	require.NoError(t, err)

	upgradeTx, err := signer.CreateTx(
		[]sdk.Msg{
			signaltypes.NewMsgSignalVersion(valAddr, 3),
			signaltypes.NewMsgTryUpgrade(accAddr),
		},
		user.SetGasLimitAndGasPrice(100_000, appconsts.DefaultMinGasPrice),
	)
	require.NoError(t, err)
	testApp.BeginBlock(abci.RequestBeginBlock{
		Header: tmproto.Header{
			ChainID: genesis.ChainID,
			Height:  3,
			Version: tmversion.Consensus{App: 2},
		},
	})

	deliverTxResp := testApp.DeliverTx(abci.RequestDeliverTx{
		Tx: upgradeTx,
	})
	require.Equal(t, abci.CodeTypeOK, deliverTxResp.Code, deliverTxResp.Log)

	endBlockResp := testApp.EndBlock(abci.RequestEndBlock{
		Height: 3,
	})
	require.Equal(t, v2.Version, endBlockResp.ConsensusParamUpdates.Version.AppVersion)
	require.Equal(t, appconsts.GetTimeoutCommit(v2.Version),
		endBlockResp.Timeouts.TimeoutCommit)
	require.Equal(t, appconsts.GetTimeoutPropose(v2.Version),
		endBlockResp.Timeouts.TimeoutPropose)
	testApp.Commit()
	require.NoError(t, signer.IncrementSequence(testnode.DefaultValidatorAccountName))

	ctx = testApp.NewContext(true, tmproto.Header{})
	getUpgradeResp, err := testApp.SignalKeeper.GetUpgrade(ctx, &signaltypes.QueryGetUpgradeRequest{})
	require.NoError(t, err)
	require.Equal(t, v3.Version, getUpgradeResp.Upgrade.AppVersion)

	// brace yourselfs, this part may take a while
	initialHeight := int64(4)
	for height := initialHeight; height < initialHeight+appconsts.DefaultUpgradeHeightDelay; height++ {
		appVersion := v2.Version
		_ = testApp.BeginBlock(abci.RequestBeginBlock{
			Header: tmproto.Header{
				Height:  height,
				Version: tmversion.Consensus{App: appVersion},
			},
		})

		endBlockResp = testApp.EndBlock(abci.RequestEndBlock{
			Height: 3 + appconsts.DefaultUpgradeHeightDelay,
		})

		require.Equal(t, appconsts.GetTimeoutCommit(appVersion), endBlockResp.Timeouts.TimeoutCommit)
		require.Equal(t, appconsts.GetTimeoutPropose(appVersion), endBlockResp.Timeouts.TimeoutPropose)

		_ = testApp.Commit()
	}
	require.Equal(t, v3.Version, endBlockResp.ConsensusParamUpdates.Version.AppVersion)

	// confirm that an authored blob tx works
	blob, err := share.NewV1Blob(share.RandomBlobNamespace(), []byte("hello world"), accAddr.Bytes())
	require.NoError(t, err)
	blobTxBytes, _, err := signer.CreatePayForBlobs(
		testnode.DefaultValidatorAccountName,
		[]*share.Blob{blob},
		user.SetGasLimitAndGasPrice(200_000, appconsts.DefaultMinGasPrice),
	)
	require.NoError(t, err)
	blobTx, _, err := tx.UnmarshalBlobTx(blobTxBytes)
	require.NoError(t, err)

	_ = testApp.BeginBlock(abci.RequestBeginBlock{
		Header: tmproto.Header{
			ChainID: genesis.ChainID,
			Height:  initialHeight + appconsts.DefaultUpgradeHeightDelay,
			Version: tmversion.Consensus{App: 3},
		},
	})

	deliverTxResp = testApp.DeliverTx(abci.RequestDeliverTx{
		Tx: blobTx.Tx,
	})
	require.Equal(t, abci.CodeTypeOK, deliverTxResp.Code, deliverTxResp.Log)

	respEndBlock := testApp.EndBlock(abci.
		RequestEndBlock{Height: initialHeight + appconsts.DefaultUpgradeHeightDelay})
	require.Equal(t, appconsts.GetTimeoutCommit(v3.Version), respEndBlock.Timeouts.TimeoutCommit)
	require.Equal(t, appconsts.GetTimeoutPropose(v3.Version), respEndBlock.Timeouts.TimeoutPropose)

}

// TestAppUpgradeV2 verifies that the all module's params are overridden during an
// upgrade from v1 -> v2 and the app version changes correctly.
func TestAppUpgradeV2(t *testing.T) {
	NetworkMinGasPriceDec, err := sdk.NewDecFromStr(fmt.Sprintf("%f", appconsts.DefaultNetworkMinGasPrice))
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
			key:           string(minfee.KeyNetworkMinGasPrice),
			expectedValue: NetworkMinGasPriceDec.String(),
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

func SetupTestAppWithUpgradeHeight(t *testing.T, upgradeHeight int64) (*app.App, *genesis.Genesis) {
	t.Helper()

	db := dbm.NewMemDB()
	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	testApp := app.New(log.NewNopLogger(), db, nil, 0, encCfg, upgradeHeight, util.EmptyAppOptions{})
	genesis := genesis.NewDefaultGenesis().
		WithValidators(genesis.NewDefaultValidator(testnode.DefaultValidatorAccountName)).
		WithConsensusParams(app.DefaultInitialConsensusParams())
	genDoc, err := genesis.Export()
	require.NoError(t, err)
	cp := genDoc.ConsensusParams
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
			Time:            genDoc.GenesisTime,
			Validators:      []abci.ValidatorUpdate{},
			ConsensusParams: abciParams,
			AppStateBytes:   genDoc.AppState,
			ChainId:         genDoc.ChainID,
		},
	)

	// assert that the chain starts with version provided in genesis
	infoResp := testApp.Info(abci.RequestInfo{})
	require.EqualValues(t, app.DefaultInitialConsensusParams().Version.AppVersion, infoResp.AppVersion)

	supportedVersions := []uint64{v1.Version, v2.Version, v3.Version}
	require.Equal(t, supportedVersions, testApp.SupportedVersions())

	_ = testApp.Commit()
	return testApp, genesis
}

func upgradeFromV1ToV2(t *testing.T, testApp *app.App) {
	t.Helper()
	testApp.BeginBlock(abci.RequestBeginBlock{Header: tmproto.Header{
		Height:  2,
		Version: tmversion.Consensus{App: 1},
	}})
	endBlockResp := testApp.EndBlock(abci.RequestEndBlock{Height: 2})
	require.Equal(t, appconsts.GetTimeoutCommit(v2.Version),
		endBlockResp.Timeouts.TimeoutCommit)
	require.Equal(t, appconsts.GetTimeoutPropose(v2.Version),
		endBlockResp.Timeouts.TimeoutPropose)
	testApp.Commit()
	require.EqualValues(t, 2, testApp.AppVersion())
}
