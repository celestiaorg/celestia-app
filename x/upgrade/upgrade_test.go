package upgrade_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/app/encoding"
	"github.com/celestiaorg/celestia-app/test/util"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"github.com/stretchr/testify/require"
	abci "github.com/tendermint/tendermint/abci/types"
	"github.com/tendermint/tendermint/libs/log"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	dbm "github.com/tendermint/tm-db"
)

func TestUpgradeAppVersion(t *testing.T) {
	testApp, _ := setupTestApp(t, 3)

	testApp.BeginBlock(abci.RequestBeginBlock{Header: tmproto.Header{Height: 2}})
	// app version should not have changed yet
	require.EqualValues(t, 1, testApp.AppVersion())
	respEndBlock := testApp.EndBlock(abci.RequestEndBlock{Height: 2})
	// now the app version changes
	require.EqualValues(t, 2, respEndBlock.ConsensusParamUpdates.Version.AppVersion)
	require.EqualValues(t, 2, testApp.AppVersion())
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
	require.EqualValues(t, 0, testApp.GetBaseApp().AppVersion())

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
	require.EqualValues(t, app.DefaultConsensusParams().Version.AppVersion, testApp.GetBaseApp().AppVersion())

	_ = testApp.Commit()
	return testApp, kr
}
