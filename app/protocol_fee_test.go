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

// TestProcessProposalProtocolFeeValidation tests ProcessProposal validation of protocol fee transactions.
// Note: For tests that expect ACCEPT, we use PrepareProposal to get the correct DataRootHash.
// For tests that expect REJECT, the validation fails before the DataRootHash check.
func TestProcessProposalProtocolFeeValidation(t *testing.T) {
	ecfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)

	// Helper to create a protocol fee tx with specific fee
	createProtocolFeeTx := func(feeAmount sdk.Coin) []byte {
		msg := feeaddress.NewMsgPayProtocolFee()
		txBuilder := ecfg.TxConfig.NewTxBuilder()
		err := txBuilder.SetMsgs(msg)
		require.NoError(t, err)
		txBuilder.SetFeeAmount(sdk.NewCoins(feeAmount))
		txBuilder.SetGasLimit(feeaddress.ProtocolFeeGasLimit)
		txBytes, err := ecfg.TxConfig.TxEncoder()(txBuilder.GetTx())
		require.NoError(t, err)
		return txBytes
	}

	// Helper to create a protocol fee tx with wrong gas limit
	createProtocolFeeTxWrongGas := func(feeAmount sdk.Coin, gas uint64) []byte {
		msg := feeaddress.NewMsgPayProtocolFee()
		txBuilder := ecfg.TxConfig.NewTxBuilder()
		err := txBuilder.SetMsgs(msg)
		require.NoError(t, err)
		txBuilder.SetFeeAmount(sdk.NewCoins(feeAmount))
		txBuilder.SetGasLimit(gas)
		txBytes, err := ecfg.TxConfig.TxEncoder()(txBuilder.GetTx())
		require.NoError(t, err)
		return txBytes
	}

	testCases := []struct {
		name            string
		feeAddrBalance  sdk.Coin        // balance at fee address before block
		txs             func() [][]byte // transactions in the block (used for reject tests)
		usePrepProposal bool            // if true, use PrepareProposal output for accept tests
		expectReject    bool
	}{
		{
			name:            "accept valid protocol fee tx when balance exists",
			feeAddrBalance:  sdk.NewCoin(appconsts.BondDenom, math.NewInt(1000000)),
			usePrepProposal: true, // Use PrepareProposal to get correct txs and DataRootHash
			expectReject:    false,
		},
		{
			name:            "accept empty block when no balance",
			feeAddrBalance:  sdk.NewCoin(appconsts.BondDenom, math.ZeroInt()),
			usePrepProposal: true, // Use PrepareProposal to get correct DataRootHash for empty block
			expectReject:    false,
		},
		{
			name:           "reject missing protocol fee tx when balance exists",
			feeAddrBalance: sdk.NewCoin(appconsts.BondDenom, math.NewInt(1000000)),
			txs: func() [][]byte {
				return [][]byte{} // empty block, but balance exists
			},
			expectReject: true,
		},
		{
			name:           "reject protocol fee tx when no balance exists",
			feeAddrBalance: sdk.NewCoin(appconsts.BondDenom, math.ZeroInt()),
			txs: func() [][]byte {
				return [][]byte{createProtocolFeeTx(sdk.NewCoin(appconsts.BondDenom, math.NewInt(1000000)))}
			},
			expectReject: true,
		},
		{
			name:           "reject protocol fee tx with wrong fee amount",
			feeAddrBalance: sdk.NewCoin(appconsts.BondDenom, math.NewInt(1000000)),
			txs: func() [][]byte {
				// Fee amount doesn't match balance
				return [][]byte{createProtocolFeeTx(sdk.NewCoin(appconsts.BondDenom, math.NewInt(500000)))}
			},
			expectReject: true,
		},
		{
			name:           "reject protocol fee tx with wrong gas limit",
			feeAddrBalance: sdk.NewCoin(appconsts.BondDenom, math.NewInt(1000000)),
			txs: func() [][]byte {
				wrongGasLimit := uint64(feeaddress.ProtocolFeeGasLimit * 2) // Intentionally wrong
				return [][]byte{createProtocolFeeTxWrongGas(sdk.NewCoin(appconsts.BondDenom, math.NewInt(1000000)), wrongGasLimit)}
			},
			expectReject: true,
		},
		{
			name:           "reject protocol fee tx with wrong denom",
			feeAddrBalance: sdk.NewCoin(appconsts.BondDenom, math.NewInt(1000000)),
			txs: func() [][]byte {
				// Wrong denom in fee
				msg := feeaddress.NewMsgPayProtocolFee()
				txBuilder := ecfg.TxConfig.NewTxBuilder()
				err := txBuilder.SetMsgs(msg)
				require.NoError(t, err)
				txBuilder.SetFeeAmount(sdk.NewCoins(sdk.NewCoin("wrongdenom", math.NewInt(1000000))))
				txBuilder.SetGasLimit(feeaddress.ProtocolFeeGasLimit)
				txBytes, err := ecfg.TxConfig.TxEncoder()(txBuilder.GetTx())
				require.NoError(t, err)
				return [][]byte{txBytes}
			},
			expectReject: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			testApp := createTestAppWithFeeAddressBalance(t, tc.feeAddrBalance)
			blockTime := time.Now()

			if tc.usePrepProposal {
				// For accept tests, use PrepareProposal to get correct txs and DataRootHash
				prepResp, err := testApp.PrepareProposal(&abci.RequestPrepareProposal{
					Time:   blockTime,
					Height: 2,
					Txs:    [][]byte{},
				})
				require.NoError(t, err)

				// Verify PrepareProposal generated correct txs
				if !tc.feeAddrBalance.IsZero() {
					require.True(t, len(prepResp.Txs) > 0, "expected protocol fee tx when balance exists")
					firstTx, err := ecfg.TxConfig.TxDecoder()(prepResp.Txs[0])
					require.NoError(t, err)
					msgs := firstTx.GetMsgs()
					require.Len(t, msgs, 1)
					_, ok := msgs[0].(*feeaddress.MsgPayProtocolFee)
					require.True(t, ok, "first tx should be MsgPayProtocolFee")
				}

				// Process proposal with PrepareProposal output
				resp, err := testApp.ProcessProposal(&abci.RequestProcessProposal{
					Time:         blockTime,
					Height:       2,
					Txs:          prepResp.Txs,
					SquareSize:   prepResp.SquareSize,
					DataRootHash: prepResp.DataRootHash,
				})
				require.NoError(t, err)
				require.NotNil(t, resp)
				require.Equal(t, abci.ResponseProcessProposal_ACCEPT, resp.Status,
					"expected ACCEPT status for: %s", tc.name)
			} else {
				// For reject tests, ProcessProposal fails before DataRootHash verification
				resp, err := testApp.ProcessProposal(&abci.RequestProcessProposal{
					Time:       blockTime,
					Height:     2,
					Txs:        tc.txs(),
					SquareSize: 1,
				})
				require.NoError(t, err)
				require.NotNil(t, resp)
				require.Equal(t, abci.ResponseProcessProposal_REJECT, resp.Status,
					"expected REJECT status for: %s", tc.name)
			}
		})
	}
}

// TestPrepareProposalProtocolFee tests that PrepareProposal correctly injects protocol fee transactions.
func TestPrepareProposalProtocolFee(t *testing.T) {
	ecfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)

	testCases := []struct {
		name                string
		feeAddrBalance      sdk.Coin
		expectProtocolFeeTx bool
	}{
		{
			name:                "inject protocol fee tx when balance exists",
			feeAddrBalance:      sdk.NewCoin(appconsts.BondDenom, math.NewInt(1000000)),
			expectProtocolFeeTx: true,
		},
		{
			name:                "no protocol fee tx when no balance",
			feeAddrBalance:      sdk.NewCoin(appconsts.BondDenom, math.ZeroInt()),
			expectProtocolFeeTx: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			testApp := createTestAppWithFeeAddressBalance(t, tc.feeAddrBalance)

			// PrepareProposal for block 2
			resp, err := testApp.PrepareProposal(&abci.RequestPrepareProposal{
				Height: 2,
				Time:   time.Now(),
				Txs:    [][]byte{},
			})
			require.NoError(t, err)
			require.NotNil(t, resp)

			if tc.expectProtocolFeeTx {
				require.True(t, len(resp.Txs) > 0, "expected at least one tx (protocol fee)")

				// Verify first tx is protocol fee
				firstTx, err := ecfg.TxConfig.TxDecoder()(resp.Txs[0])
				require.NoError(t, err)
				msgs := firstTx.GetMsgs()
				require.Len(t, msgs, 1)
				_, ok := msgs[0].(*feeaddress.MsgPayProtocolFee)
				require.True(t, ok, "first tx should be MsgPayProtocolFee")

				// Verify fee amount matches balance
				feeTx, ok := firstTx.(sdk.FeeTx)
				require.True(t, ok)
				require.Equal(t, tc.feeAddrBalance, feeTx.GetFee()[0])

				// Verify gas limit (cast to uint64 for comparison)
				require.Equal(t, uint64(feeaddress.ProtocolFeeGasLimit), feeTx.GetGas())
			} else {
				// Verify no protocol fee tx (may have empty txs or non-protocol-fee txs)
				for _, txBytes := range resp.Txs {
					tx, err := ecfg.TxConfig.TxDecoder()(txBytes)
					if err != nil {
						continue // skip malformed txs
					}
					msgs := tx.GetMsgs()
					if len(msgs) == 1 {
						_, ok := msgs[0].(*feeaddress.MsgPayProtocolFee)
						require.False(t, ok, "should not have protocol fee tx when no balance")
					}
				}
			}
		})
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
