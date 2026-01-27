package app_test

import (
	"encoding/json"
	"testing"
	"time"

	"cosmossdk.io/log"
	"cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app/v7/app"
	"github.com/celestiaorg/celestia-app/v7/app/encoding"
	"github.com/celestiaorg/celestia-app/v7/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v7/pkg/feeaddress"
	"github.com/celestiaorg/celestia-app/v7/test/util"
	"github.com/celestiaorg/celestia-app/v7/test/util/testfactory"
	abci "github.com/cometbft/cometbft/abci/types"
	tmdb "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/baseapp"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"
)

// createTestAppWithFeeAddressBalance creates a new app with an optional fee address balance,
// initializes the chain, and finalizes block 1. The app is ready for block 2 operations.
func createTestAppWithFeeAddressBalance(t *testing.T, feeAddrBalance sdk.Coin) *app.App {
	t.Helper()
	logger := log.NewNopLogger()
	db := tmdb.NewMemDB()
	traceStore := &NoopWriter{}
	appOptions := NoopAppOptions{}
	testApp := app.New(logger, db, traceStore, time.Second, appOptions, baseapp.SetChainID(testfactory.ChainID))

	// Initialize chain
	genesisState, _, _ := util.GenesisStateWithSingleValidator(testApp, "validator")

	// Modify genesis to fund the fee address if needed
	if !feeAddrBalance.IsZero() {
		var bankGenesis banktypes.GenesisState
		err := json.Unmarshal(genesisState[banktypes.ModuleName], &bankGenesis)
		require.NoError(t, err)

		bankGenesis.Balances = append(bankGenesis.Balances, banktypes.Balance{
			Address: feeaddress.FeeAddressBech32,
			Coins:   sdk.NewCoins(feeAddrBalance),
		})
		bankGenesis.Supply = bankGenesis.Supply.Add(feeAddrBalance)

		bankGenesisBytes, err := json.Marshal(bankGenesis)
		require.NoError(t, err)
		genesisState[banktypes.ModuleName] = bankGenesisBytes
	}

	appStateBytes, err := json.MarshalIndent(genesisState, "", " ")
	require.NoError(t, err)

	_, err = testApp.InitChain(&abci.RequestInitChain{
		Time:            time.Now(),
		ChainId:         testfactory.ChainID,
		AppStateBytes:   appStateBytes,
		InitialHeight:   1,
		ConsensusParams: app.DefaultConsensusParams(),
	})
	require.NoError(t, err)

	// FinalizeBlock for block 1 to complete initialization
	_, err = testApp.FinalizeBlock(&abci.RequestFinalizeBlock{
		Height: 1,
		Time:   time.Now(),
		Txs:    [][]byte{},
	})
	require.NoError(t, err)
	_, err = testApp.Commit()
	require.NoError(t, err)

	return testApp
}

// createProtocolFeeTx creates a protocol fee tx with the specified fee amount and gas limit.
func createProtocolFeeTx(t *testing.T, feeAmount sdk.Coin, gasLimit uint64) []byte {
	t.Helper()
	ecfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	msg := feeaddress.NewMsgPayProtocolFee()
	txBuilder := ecfg.TxConfig.NewTxBuilder()
	err := txBuilder.SetMsgs(msg)
	require.NoError(t, err)
	txBuilder.SetFeeAmount(sdk.NewCoins(feeAmount))
	txBuilder.SetGasLimit(gasLimit)
	txBytes, err := ecfg.TxConfig.TxEncoder()(txBuilder.GetTx())
	require.NoError(t, err)
	return txBytes
}

// TestProcessProposalAcceptsValidProtocolFeeTx verifies ProcessProposal accepts
// blocks with valid protocol fee tx when fee address has balance.
func TestProcessProposalAcceptsValidProtocolFeeTx(t *testing.T) {
	ecfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	feeAddrBalance := sdk.NewCoin(appconsts.BondDenom, math.NewInt(1000000))
	testApp := createTestAppWithFeeAddressBalance(t, feeAddrBalance)
	blockTime := time.Now()

	prepResp, err := testApp.PrepareProposal(&abci.RequestPrepareProposal{
		Time:   blockTime,
		Height: 2,
		Txs:    [][]byte{},
	})
	require.NoError(t, err)
	require.True(t, len(prepResp.Txs) > 0, "expected protocol fee tx when balance exists")

	// Verify first tx is MsgPayProtocolFee
	firstTx, err := ecfg.TxConfig.TxDecoder()(prepResp.Txs[0])
	require.NoError(t, err)
	msgs := firstTx.GetMsgs()
	require.Len(t, msgs, 1)
	_, ok := msgs[0].(*feeaddress.MsgPayProtocolFee)
	require.True(t, ok, "first tx should be MsgPayProtocolFee")

	resp, err := testApp.ProcessProposal(&abci.RequestProcessProposal{
		Time:         blockTime,
		Height:       2,
		Txs:          prepResp.Txs,
		SquareSize:   prepResp.SquareSize,
		DataRootHash: prepResp.DataRootHash,
	})
	require.NoError(t, err)
	require.Equal(t, abci.ResponseProcessProposal_ACCEPT, resp.Status)
}

// TestProcessProposalAcceptsEmptyBlockWhenNoBalance verifies ProcessProposal accepts
// empty blocks when fee address has no balance.
func TestProcessProposalAcceptsEmptyBlockWhenNoBalance(t *testing.T) {
	testApp := createTestAppWithFeeAddressBalance(t, sdk.NewCoin(appconsts.BondDenom, math.ZeroInt()))
	blockTime := time.Now()

	prepResp, err := testApp.PrepareProposal(&abci.RequestPrepareProposal{
		Time:   blockTime,
		Height: 2,
		Txs:    [][]byte{},
	})
	require.NoError(t, err)

	resp, err := testApp.ProcessProposal(&abci.RequestProcessProposal{
		Time:         blockTime,
		Height:       2,
		Txs:          prepResp.Txs,
		SquareSize:   prepResp.SquareSize,
		DataRootHash: prepResp.DataRootHash,
	})
	require.NoError(t, err)
	require.Equal(t, abci.ResponseProcessProposal_ACCEPT, resp.Status)
}

// TestProcessProposalRejectsInvalidProtocolFeeTx tests that ProcessProposal rejects
// blocks with invalid or missing protocol fee transactions.
func TestProcessProposalRejectsInvalidProtocolFeeTx(t *testing.T) {
	ecfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	balance := sdk.NewCoin(appconsts.BondDenom, math.NewInt(1000000))

	testCases := []struct {
		name           string
		feeAddrBalance sdk.Coin
		txs            [][]byte
	}{
		{
			name:           "missing protocol fee tx when balance exists",
			feeAddrBalance: balance,
			txs:            [][]byte{},
		},
		{
			name:           "protocol fee tx when no balance exists",
			feeAddrBalance: sdk.NewCoin(appconsts.BondDenom, math.ZeroInt()),
			txs:            [][]byte{createProtocolFeeTx(t, balance, feeaddress.ProtocolFeeGasLimit)},
		},
		{
			name:           "wrong fee amount",
			feeAddrBalance: balance,
			txs:            [][]byte{createProtocolFeeTx(t, sdk.NewCoin(appconsts.BondDenom, math.NewInt(500000)), feeaddress.ProtocolFeeGasLimit)},
		},
		{
			name:           "wrong gas limit",
			feeAddrBalance: balance,
			txs:            [][]byte{createProtocolFeeTx(t, balance, feeaddress.ProtocolFeeGasLimit*2)},
		},
		{
			name:           "wrong denom",
			feeAddrBalance: balance,
			txs: func() [][]byte {
				msg := feeaddress.NewMsgPayProtocolFee()
				txBuilder := ecfg.TxConfig.NewTxBuilder()
				_ = txBuilder.SetMsgs(msg)
				txBuilder.SetFeeAmount(sdk.NewCoins(sdk.NewCoin("wrongdenom", math.NewInt(1000000))))
				txBuilder.SetGasLimit(feeaddress.ProtocolFeeGasLimit)
				txBytes, _ := ecfg.TxConfig.TxEncoder()(txBuilder.GetTx())
				return [][]byte{txBytes}
			}(),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			testApp := createTestAppWithFeeAddressBalance(t, tc.feeAddrBalance)

			resp, err := testApp.ProcessProposal(&abci.RequestProcessProposal{
				Time:       time.Now(),
				Height:     2,
				Txs:        tc.txs,
				SquareSize: 1,
			})
			require.NoError(t, err)
			require.Equal(t, abci.ResponseProcessProposal_REJECT, resp.Status)
		})
	}
}

// TestPrepareProposalInjectsProtocolFeeTx verifies that PrepareProposal injects
// a protocol fee tx when the fee address has a balance.
func TestPrepareProposalInjectsProtocolFeeTx(t *testing.T) {
	ecfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	feeAddrBalance := sdk.NewCoin(appconsts.BondDenom, math.NewInt(1000000))
	testApp := createTestAppWithFeeAddressBalance(t, feeAddrBalance)

	resp, err := testApp.PrepareProposal(&abci.RequestPrepareProposal{
		Height: 2,
		Time:   time.Now(),
		Txs:    [][]byte{},
	})
	require.NoError(t, err)
	require.True(t, len(resp.Txs) > 0, "expected protocol fee tx")

	// Verify first tx is MsgPayProtocolFee with correct fee and gas
	firstTx, err := ecfg.TxConfig.TxDecoder()(resp.Txs[0])
	require.NoError(t, err)
	msgs := firstTx.GetMsgs()
	require.Len(t, msgs, 1)
	_, ok := msgs[0].(*feeaddress.MsgPayProtocolFee)
	require.True(t, ok, "first tx should be MsgPayProtocolFee")

	feeTx, ok := firstTx.(sdk.FeeTx)
	require.True(t, ok)
	require.Equal(t, feeAddrBalance, feeTx.GetFee()[0])
	require.Equal(t, uint64(feeaddress.ProtocolFeeGasLimit), feeTx.GetGas())
}

// TestPrepareProposalNoProtocolFeeTxWhenNoBalance verifies that PrepareProposal
// does not inject a protocol fee tx when the fee address has no balance.
func TestPrepareProposalNoProtocolFeeTxWhenNoBalance(t *testing.T) {
	ecfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	testApp := createTestAppWithFeeAddressBalance(t, sdk.NewCoin(appconsts.BondDenom, math.ZeroInt()))

	resp, err := testApp.PrepareProposal(&abci.RequestPrepareProposal{
		Height: 2,
		Time:   time.Now(),
		Txs:    [][]byte{},
	})
	require.NoError(t, err)

	// Verify no protocol fee tx in response
	for _, txBytes := range resp.Txs {
		tx, err := ecfg.TxConfig.TxDecoder()(txBytes)
		require.NoError(t, err)
		msgs := tx.GetMsgs()
		for _, msg := range msgs {
			_, ok := msg.(*feeaddress.MsgPayProtocolFee)
			require.False(t, ok, "should not have protocol fee tx when no balance")
		}
	}
}

// TestProtocolFeeGasConsumption verifies that actual gas consumption for protocol fee
// transactions stays well below the ProtocolFeeGasLimit constant.
func TestProtocolFeeGasConsumption(t *testing.T) {
	feeAddrBalance := sdk.NewCoin(appconsts.BondDenom, math.NewInt(1000000))
	testApp := createTestAppWithFeeAddressBalance(t, feeAddrBalance)

	// PrepareProposal for block 2 to get the protocol fee tx
	blockTime := time.Now()
	prepareResp, err := testApp.PrepareProposal(&abci.RequestPrepareProposal{
		Height: 2,
		Time:   blockTime,
		Txs:    [][]byte{},
	})
	require.NoError(t, err)
	require.True(t, len(prepareResp.Txs) > 0, "expected protocol fee tx")

	// FinalizeBlock to execute the protocol fee tx and get gas consumption
	finalizeResp, err := testApp.FinalizeBlock(&abci.RequestFinalizeBlock{
		Height: 2,
		Time:   blockTime,
		Txs:    prepareResp.Txs,
	})
	require.NoError(t, err)
	require.True(t, len(finalizeResp.TxResults) > 0, "expected at least one tx result")

	// The first tx result should be the protocol fee tx
	protocolFeeResult := finalizeResp.TxResults[0]
	require.Equal(t, uint32(0), protocolFeeResult.Code, "protocol fee tx should succeed")

	// Verify gas consumption is well below the limit (at least 20% margin)
	gasUsed := protocolFeeResult.GasUsed
	gasLimit := uint64(feeaddress.ProtocolFeeGasLimit)
	maxExpectedGas := gasLimit * 80 / 100 // 80% of limit

	t.Logf("Protocol fee gas consumption: used=%d, limit=%d, max_expected=%d",
		gasUsed, gasLimit, maxExpectedGas)

	require.Less(t, uint64(gasUsed), gasLimit,
		"gas used (%d) should be less than gas limit (%d)", gasUsed, gasLimit)
	require.Less(t, uint64(gasUsed), maxExpectedGas,
		"gas used (%d) should be well below gas limit (<%d) to ensure safety margin",
		gasUsed, maxExpectedGas)
}

// TestPrepareProposalProcessProposalRoundTrip verifies that a block created by
// PrepareProposal is accepted by ProcessProposal.
func TestPrepareProposalProcessProposalRoundTrip(t *testing.T) {
	feeAddrBalance := sdk.NewCoin(appconsts.BondDenom, math.NewInt(5000000))
	testApp := createTestAppWithFeeAddressBalance(t, feeAddrBalance)

	// PrepareProposal for block 2
	blockTime := time.Now()
	prepareResp, err := testApp.PrepareProposal(&abci.RequestPrepareProposal{
		Height: 2,
		Time:   blockTime,
		Txs:    [][]byte{},
	})
	require.NoError(t, err)
	require.NotNil(t, prepareResp)

	// ProcessProposal with the same txs and DataRootHash from PrepareProposal
	processResp, err := testApp.ProcessProposal(&abci.RequestProcessProposal{
		Height:       2,
		Time:         blockTime,
		Txs:          prepareResp.Txs,
		SquareSize:   prepareResp.SquareSize,
		DataRootHash: prepareResp.DataRootHash,
	})
	require.NoError(t, err)
	require.NotNil(t, processResp)
	require.Equal(t, abci.ResponseProcessProposal_ACCEPT, processResp.Status,
		"ProcessProposal should accept block created by PrepareProposal")
}
