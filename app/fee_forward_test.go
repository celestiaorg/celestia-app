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
	"github.com/celestiaorg/celestia-app/v7/test/util"
	"github.com/celestiaorg/celestia-app/v7/test/util/testfactory"
	feeaddresstypes "github.com/celestiaorg/celestia-app/v7/x/feeaddress/types"
	abci "github.com/cometbft/cometbft/abci/types"
	tmdb "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/baseapp"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"
)

// TestProcessProposalFeeForwardValidation tests ProcessProposal validation of fee forward transactions.
// Note: For tests that expect ACCEPT, we use PrepareProposal to get the correct DataRootHash.
// For tests that expect REJECT, the validation fails before the DataRootHash check.
func TestProcessProposalFeeForwardValidation(t *testing.T) {
	ecfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)

	// Helper to create a fee forward tx with specific fee
	createFeeForwardTx := func(feeAmount sdk.Coin) []byte {
		msg := feeaddresstypes.NewMsgForwardFees()
		txBuilder := ecfg.TxConfig.NewTxBuilder()
		err := txBuilder.SetMsgs(msg)
		require.NoError(t, err)
		txBuilder.SetFeeAmount(sdk.NewCoins(feeAmount))
		txBuilder.SetGasLimit(feeaddresstypes.FeeForwardGasLimit)
		txBytes, err := ecfg.TxConfig.TxEncoder()(txBuilder.GetTx())
		require.NoError(t, err)
		return txBytes
	}

	// Helper to create a fee forward tx with wrong gas limit
	createFeeForwardTxWrongGas := func(feeAmount sdk.Coin, gas uint64) []byte {
		msg := feeaddresstypes.NewMsgForwardFees()
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
		feeAddrBalance  sdk.Coin                // balance at fee address before block
		txs             func() [][]byte         // transactions in the block (used for reject tests)
		usePrepProposal bool                    // if true, use PrepareProposal output for accept tests
		expectReject    bool
	}{
		{
			name:            "accept valid fee forward tx when balance exists",
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
			name:           "reject missing fee forward tx when balance exists",
			feeAddrBalance: sdk.NewCoin(appconsts.BondDenom, math.NewInt(1000000)),
			txs: func() [][]byte {
				return [][]byte{} // empty block, but balance exists
			},
			expectReject: true,
		},
		{
			name:           "reject fee forward tx when no balance exists",
			feeAddrBalance: sdk.NewCoin(appconsts.BondDenom, math.ZeroInt()),
			txs: func() [][]byte {
				return [][]byte{createFeeForwardTx(sdk.NewCoin(appconsts.BondDenom, math.NewInt(1000000)))}
			},
			expectReject: true,
		},
		{
			name:           "reject fee forward tx with wrong fee amount",
			feeAddrBalance: sdk.NewCoin(appconsts.BondDenom, math.NewInt(1000000)),
			txs: func() [][]byte {
				// Fee amount doesn't match balance
				return [][]byte{createFeeForwardTx(sdk.NewCoin(appconsts.BondDenom, math.NewInt(500000)))}
			},
			expectReject: true,
		},
		{
			name:           "reject fee forward tx with wrong gas limit",
			feeAddrBalance: sdk.NewCoin(appconsts.BondDenom, math.NewInt(1000000)),
			txs: func() [][]byte {
				return [][]byte{createFeeForwardTxWrongGas(sdk.NewCoin(appconsts.BondDenom, math.NewInt(1000000)), 100000)}
			},
			expectReject: true,
		},
		{
			name:           "reject fee forward tx with wrong denom",
			feeAddrBalance: sdk.NewCoin(appconsts.BondDenom, math.NewInt(1000000)),
			txs: func() [][]byte {
				// Wrong denom in fee
				msg := feeaddresstypes.NewMsgForwardFees()
				txBuilder := ecfg.TxConfig.NewTxBuilder()
				err := txBuilder.SetMsgs(msg)
				require.NoError(t, err)
				txBuilder.SetFeeAmount(sdk.NewCoins(sdk.NewCoin("wrongdenom", math.NewInt(1000000))))
				txBuilder.SetGasLimit(feeaddresstypes.FeeForwardGasLimit)
				txBytes, err := ecfg.TxConfig.TxEncoder()(txBuilder.GetTx())
				require.NoError(t, err)
				return [][]byte{txBytes}
			},
			expectReject: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create a new app for each test
			logger := log.NewNopLogger()
			db := tmdb.NewMemDB()
			traceStore := &NoopWriter{}
			appOptions := NoopAppOptions{}
			testApp := app.New(logger, db, traceStore, time.Second, appOptions, baseapp.SetChainID(testfactory.ChainID))

			// Initialize chain
			genesisState, _, _ := util.GenesisStateWithSingleValidator(testApp, "validator")

			// Modify genesis to fund the fee address if needed
			if !tc.feeAddrBalance.IsZero() {
				var bankGenesis banktypes.GenesisState
				err := json.Unmarshal(genesisState[banktypes.ModuleName], &bankGenesis)
				require.NoError(t, err)

				// Add balance to fee address
				bankGenesis.Balances = append(bankGenesis.Balances, banktypes.Balance{
					Address: feeaddresstypes.FeeAddressBech32,
					Coins:   sdk.NewCoins(tc.feeAddrBalance),
				})
				bankGenesis.Supply = bankGenesis.Supply.Add(tc.feeAddrBalance)

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
					require.True(t, len(prepResp.Txs) > 0, "expected fee forward tx when balance exists")
					firstTx, err := ecfg.TxConfig.TxDecoder()(prepResp.Txs[0])
					require.NoError(t, err)
					msgs := firstTx.GetMsgs()
					require.Len(t, msgs, 1)
					_, ok := msgs[0].(*feeaddresstypes.MsgForwardFees)
					require.True(t, ok, "first tx should be MsgForwardFees")
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

// TestPrepareProposalFeeForward tests that PrepareProposal correctly injects fee forward transactions.
func TestPrepareProposalFeeForward(t *testing.T) {
	ecfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)

	testCases := []struct {
		name              string
		feeAddrBalance    sdk.Coin
		expectFeeForwardTx bool
	}{
		{
			name:              "inject fee forward tx when balance exists",
			feeAddrBalance:    sdk.NewCoin(appconsts.BondDenom, math.NewInt(1000000)),
			expectFeeForwardTx: true,
		},
		{
			name:              "no fee forward tx when no balance",
			feeAddrBalance:    sdk.NewCoin(appconsts.BondDenom, math.ZeroInt()),
			expectFeeForwardTx: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create a new app for each test
			logger := log.NewNopLogger()
			db := tmdb.NewMemDB()
			traceStore := &NoopWriter{}
			appOptions := NoopAppOptions{}
			testApp := app.New(logger, db, traceStore, time.Second, appOptions, baseapp.SetChainID(testfactory.ChainID))

			// Initialize chain
			genesisState, _, _ := util.GenesisStateWithSingleValidator(testApp, "validator")

			// Modify genesis to fund the fee address if needed
			if !tc.feeAddrBalance.IsZero() {
				var bankGenesis banktypes.GenesisState
				err := json.Unmarshal(genesisState[banktypes.ModuleName], &bankGenesis)
				require.NoError(t, err)

				// Add balance to fee address
				bankGenesis.Balances = append(bankGenesis.Balances, banktypes.Balance{
					Address: feeaddresstypes.FeeAddressBech32,
					Coins:   sdk.NewCoins(tc.feeAddrBalance),
				})
				bankGenesis.Supply = bankGenesis.Supply.Add(tc.feeAddrBalance)

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

			// PrepareProposal for block 2
			resp, err := testApp.PrepareProposal(&abci.RequestPrepareProposal{
				Height: 2,
				Time:   time.Now(),
				Txs:    [][]byte{},
			})
			require.NoError(t, err)
			require.NotNil(t, resp)

			if tc.expectFeeForwardTx {
				require.True(t, len(resp.Txs) > 0, "expected at least one tx (fee forward)")

				// Verify first tx is fee forward
				firstTx, err := ecfg.TxConfig.TxDecoder()(resp.Txs[0])
				require.NoError(t, err)
				msgs := firstTx.GetMsgs()
				require.Len(t, msgs, 1)
				_, ok := msgs[0].(*feeaddresstypes.MsgForwardFees)
				require.True(t, ok, "first tx should be MsgForwardFees")

				// Verify fee amount matches balance
				feeTx, ok := firstTx.(sdk.FeeTx)
				require.True(t, ok)
				require.Equal(t, tc.feeAddrBalance, feeTx.GetFee()[0])

				// Verify gas limit (cast to uint64 for comparison)
				require.Equal(t, uint64(feeaddresstypes.FeeForwardGasLimit), feeTx.GetGas())
			} else {
				// Verify no fee forward tx (may have empty txs or non-fee-forward txs)
				for _, txBytes := range resp.Txs {
					tx, err := ecfg.TxConfig.TxDecoder()(txBytes)
					if err != nil {
						continue // skip malformed txs
					}
					msgs := tx.GetMsgs()
					if len(msgs) == 1 {
						_, ok := msgs[0].(*feeaddresstypes.MsgForwardFees)
						require.False(t, ok, "should not have fee forward tx when no balance")
					}
				}
			}
		})
	}
}

// TestPrepareProposalProcessProposalRoundTrip verifies that a block created by
// PrepareProposal is accepted by ProcessProposal.
func TestPrepareProposalProcessProposalRoundTrip(t *testing.T) {
	// Create app
	logger := log.NewNopLogger()
	db := tmdb.NewMemDB()
	traceStore := &NoopWriter{}
	appOptions := NoopAppOptions{}
	testApp := app.New(logger, db, traceStore, time.Second, appOptions, baseapp.SetChainID(testfactory.ChainID))

	// Initialize chain with fee address having a balance
	genesisState, _, _ := util.GenesisStateWithSingleValidator(testApp, "validator")

	var bankGenesis banktypes.GenesisState
	err := json.Unmarshal(genesisState[banktypes.ModuleName], &bankGenesis)
	require.NoError(t, err)

	feeAddrBalance := sdk.NewCoin(appconsts.BondDenom, math.NewInt(5000000))
	bankGenesis.Balances = append(bankGenesis.Balances, banktypes.Balance{
		Address: feeaddresstypes.FeeAddressBech32,
		Coins:   sdk.NewCoins(feeAddrBalance),
	})
	bankGenesis.Supply = bankGenesis.Supply.Add(feeAddrBalance)

	bankGenesisBytes, err := json.Marshal(bankGenesis)
	require.NoError(t, err)
	genesisState[banktypes.ModuleName] = bankGenesisBytes

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

	// FinalizeBlock for block 1
	_, err = testApp.FinalizeBlock(&abci.RequestFinalizeBlock{
		Height: 1,
		Time:   time.Now(),
		Txs:    [][]byte{},
	})
	require.NoError(t, err)
	_, err = testApp.Commit()
	require.NoError(t, err)

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
