package upgrade_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/app/encoding"
	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/pkg/da"
	"github.com/celestiaorg/celestia-app/pkg/shares"
	"github.com/celestiaorg/celestia-app/pkg/square"
	"github.com/celestiaorg/celestia-app/pkg/user"
	"github.com/celestiaorg/celestia-app/test/util"
	"github.com/celestiaorg/celestia-app/test/util/testfactory"
	"github.com/celestiaorg/celestia-app/x/upgrade"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"github.com/cosmos/cosmos-sdk/types"
	bank "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"
	abci "github.com/tendermint/tendermint/abci/types"
	"github.com/tendermint/tendermint/libs/log"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	dbm "github.com/tendermint/tm-db"
)

func TestUpgradeAppVersion(t *testing.T) {
	testApp, kr := setupTestApp(t, upgrade.NewSchedule(upgrade.NewPlan(3, 5, 2)))
	addr := testfactory.GetAddress(kr, "account")
	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	signer, err := user.NewSigner(kr, nil, addr, encCfg.TxConfig, testApp.GetChainID(), 1, 0)
	require.NoError(t, err)
	coins := types.NewCoins(types.NewCoin("utia", types.NewInt(10)))
	sendMsg := bank.NewMsgSend(addr, addr, coins)
	sendTx, err := signer.CreateTx([]types.Msg{sendMsg}, user.SetGasLimitAndFee(1e6, 1))
	require.NoError(t, err)

	upgradeTx, err := upgrade.NewMsgVersionChange(testApp.GetTxConfig(), 3)
	require.NoError(t, err)
	respCheckTx := testApp.CheckTx(abci.RequestCheckTx{Tx: upgradeTx})
	// we expect that a new msg version change should always be rejected
	// by checkTx
	require.EqualValues(t, 15, respCheckTx.Code, respCheckTx.Log)

	resp := testApp.PrepareProposal(abci.RequestPrepareProposal{
		Height:        2,
		ChainId:       testApp.GetChainID(),
		BlockData:     &tmproto.Data{},
		BlockDataSize: 1e6,
	})

	// At the height before the first height in the upgrade plan, the
	// node should prepend a signal upgrade message.
	require.Len(t, resp.BlockData.Txs, 1)
	tx, err := testApp.GetTxConfig().TxDecoder()(resp.BlockData.Txs[0])
	require.NoError(t, err)
	require.Len(t, tx.GetMsgs(), 1)
	msg, ok := tx.GetMsgs()[0].(*upgrade.MsgVersionChange)
	require.True(t, ok)
	require.EqualValues(t, 2, msg.Version)

	{
		// the same thing should happen if we run prepare proposal
		// at height 4 as it is within the range
		resp := testApp.PrepareProposal(abci.RequestPrepareProposal{
			Height:        4,
			ChainId:       testApp.GetChainID(),
			BlockData:     &tmproto.Data{},
			BlockDataSize: 1e6,
		})

		require.Len(t, resp.BlockData.Txs, 1)
		tx, err := testApp.GetTxConfig().TxDecoder()(resp.BlockData.Txs[0])
		require.NoError(t, err)
		require.Len(t, tx.GetMsgs(), 1)
		msg, ok := tx.GetMsgs()[0].(*upgrade.MsgVersionChange)
		require.True(t, ok)
		require.EqualValues(t, 2, msg.Version)
	}

	{
		// we send the same proposal but now with an existing message and a
		// smaller BlockDataSize. It should kick out the tx in place of the
		// upgrade tx
		resp := testApp.PrepareProposal(abci.RequestPrepareProposal{
			Height:        2,
			ChainId:       testApp.GetChainID(),
			BlockData:     &tmproto.Data{Txs: [][]byte{sendTx}},
			BlockDataSize: 1e2,
		})
		require.Len(t, resp.BlockData.Txs, 1)
		tx, err := testApp.GetTxConfig().TxDecoder()(resp.BlockData.Txs[0])
		require.NoError(t, err)
		require.Len(t, tx.GetMsgs(), 1)
		msg, ok := tx.GetMsgs()[0].(*upgrade.MsgVersionChange)
		require.True(t, ok)
		require.EqualValues(t, 2, msg.Version)
	}

	{
		// Height 5 however is outside the range and thus the upgrade
		// message should not be prepended
		resp := testApp.PrepareProposal(abci.RequestPrepareProposal{
			Height:        5,
			ChainId:       testApp.GetChainID(),
			BlockData:     &tmproto.Data{},
			BlockDataSize: 1e6,
		})
		require.Len(t, resp.BlockData.Txs, 0)
	}

	// We should accept this proposal as valid
	processProposalResp := testApp.ProcessProposal(abci.RequestProcessProposal{
		Header: tmproto.Header{
			Height:   2,
			DataHash: resp.BlockData.Hash,
		},
		BlockData: resp.BlockData,
	})
	require.True(t, processProposalResp.IsOK())

	{
		// to assert that the upgrade tx must be the first tx
		// we insert a tx before the upgrade tx. To get the hash
		// we need to build the square and data availability header
		txs := [][]byte{[]byte("hello world"), upgradeTx}
		dataSquare, txs, err := square.Build(txs, appconsts.LatestVersion, appconsts.DefaultGovMaxSquareSize)
		require.NoError(t, err)
		eds, err := da.ExtendShares(shares.ToBytes(dataSquare))
		require.NoError(t, err)
		dah, err := da.NewDataAvailabilityHeader(eds)
		require.NoError(t, err)
		blockData := &tmproto.Data{
			Txs:        txs,
			SquareSize: uint64(dataSquare.Size()),
			Hash:       dah.Hash(),
		}

		processProposalResp := testApp.ProcessProposal(abci.RequestProcessProposal{
			Header: tmproto.Header{
				Height:   2,
				DataHash: blockData.Hash,
			},
			BlockData: blockData,
		})
		require.True(t, processProposalResp.IsRejected())
	}

	testApp.BeginBlock(abci.RequestBeginBlock{Header: tmproto.Header{Height: 2}})
	respDeliverTx := testApp.DeliverTx(abci.RequestDeliverTx{Tx: resp.BlockData.Txs[0]})
	require.EqualValues(t, 0, respDeliverTx.Code, respDeliverTx.Log)
	// app version should not have changed yet
	require.EqualValues(t, 1, testApp.AppVersion())
	respEndBlock := testApp.EndBlock(abci.RequestEndBlock{Height: 2})
	// now the app version changes
	require.EqualValues(t, 2, respEndBlock.ConsensusParamUpdates.Version.AppVersion)
	require.EqualValues(t, 2, testApp.AppVersion())

	_ = testApp.Commit()

	// If another node proposes a block with a version change that is
	// not supported by the nodes own state machine then the node
	// rejects the proposed block
	respProcessProposal := testApp.ProcessProposal(abci.RequestProcessProposal{
		Header: tmproto.Header{
			Height: 3,
		},
		BlockData: &tmproto.Data{
			Txs: [][]byte{upgradeTx},
		},
	})
	require.True(t, respProcessProposal.IsRejected())

	// if we ask the application to prepare another proposal
	// it will not add the upgrade signal message even though
	// its within the range of the plan because the application
	// has already upgraded to that height
	respPrepareProposal := testApp.PrepareProposal(abci.RequestPrepareProposal{
		Height:        3,
		ChainId:       testApp.GetChainID(),
		BlockData:     &tmproto.Data{},
		BlockDataSize: 1e6,
	})
	require.Len(t, respPrepareProposal.BlockData.Txs, 0)
}

func setupTestApp(t *testing.T, schedule upgrade.Schedule) (*app.App, keyring.Keyring) {
	t.Helper()

	db := dbm.NewMemDB()
	chainID := "test_chain"
	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	upgradeSchedule := make(map[string]upgrade.Schedule)
	upgradeSchedule[chainID] = schedule
	testApp := app.New(log.NewNopLogger(), db, nil, true, 0, encCfg, upgradeSchedule, util.EmptyAppOptions{})

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
