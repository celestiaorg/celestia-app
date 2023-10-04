package upgrade_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/app/encoding"
	"github.com/celestiaorg/celestia-app/test/util"
	"github.com/celestiaorg/celestia-app/x/upgrade"
	"github.com/stretchr/testify/require"
	abci "github.com/tendermint/tendermint/abci/types"
	"github.com/tendermint/tendermint/libs/log"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	dbm "github.com/tendermint/tm-db"
)

func TestUpgradeAppVersion(t *testing.T) {
	db := dbm.NewMemDB()
	chainID := "test_chain"
	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	upgradeSchedule := make(map[string]upgrade.Schedule)
	upgradeSchedule[chainID] = upgrade.NewSchedule(upgrade.NewPlan(3, 5, 2))
	testApp := app.New(log.NewNopLogger(), db, nil, true, 0, encCfg, upgradeSchedule, util.EmptyAppOptions{})

	genesisState, _, _ := util.GenesisStateWithSingleValidator(testApp)

	stateBytes, err := json.MarshalIndent(genesisState, "", " ")
	if err != nil {
		panic(err)
	}

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

	resp := testApp.PrepareProposal(abci.RequestPrepareProposal{
		Height:        2,
		ChainId:       chainID,
		BlockData:     &tmproto.Data{},
		BlockDataSize: 1e6,
	})

	require.Len(t, resp.BlockData.Txs, 1)

	tx, err := encCfg.TxConfig.TxDecoder()(resp.BlockData.Txs[0])
	require.NoError(t, err)
	require.Len(t, tx.GetMsgs(), 1)
	msg, ok := tx.GetMsgs()[0].(*upgrade.MsgVersionChange)
	require.True(t, ok)
	require.EqualValues(t, 2, msg.Version)

	processProposalResp := testApp.ProcessProposal(abci.RequestProcessProposal{
		Header: tmproto.Header{
			Height:   2,
			DataHash: resp.BlockData.Hash,
		},
		BlockData: resp.BlockData,
	})
	require.True(t, processProposalResp.IsOK())

	testApp.BeginBlock(abci.RequestBeginBlock{Header: tmproto.Header{Height: 2}})
	respDeliverTx := testApp.DeliverTx(abci.RequestDeliverTx{Tx: resp.BlockData.Txs[0]})
	require.EqualValues(t, 0, respDeliverTx.Code, respDeliverTx.Log)
	respEndBlock := testApp.EndBlock(abci.RequestEndBlock{Height: 2})
	require.EqualValues(t, 2, respEndBlock.ConsensusParamUpdates.Version.AppVersion)
}
