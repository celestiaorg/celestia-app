package app_test

import (
	"math/rand"
	"sync"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v8/app"
	"github.com/celestiaorg/celestia-app/v8/app/encoding"
	"github.com/celestiaorg/celestia-app/v8/app/grpc/gasestimation"
	"github.com/celestiaorg/celestia-app/v8/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v8/pkg/user"
	testutil "github.com/celestiaorg/celestia-app/v8/test/util"
	"github.com/celestiaorg/celestia-app/v8/test/util/blobfactory"
	"github.com/celestiaorg/celestia-app/v8/test/util/random"
	"github.com/celestiaorg/celestia-app/v8/test/util/testfactory"
	"github.com/celestiaorg/celestia-app/v8/test/util/testnode"
	blobtypes "github.com/celestiaorg/celestia-app/v8/x/blob/types"
	"github.com/celestiaorg/go-square/v4/share"
	abci "github.com/cometbft/cometbft/abci/types"
	coretypes "github.com/cometbft/cometbft/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSortAndExtractGasPrice(t *testing.T) {
	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...).TxConfig
	accounts := testfactory.GenerateAccounts(4)
	testApp, kr := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams(), accounts...)
	infos := queryAccountInfo(testApp, accounts, kr)
	blobs := blobfactory.NestedBlobs(
		t,
		testfactory.RandomBlobNamespaces(random.New(), 4),
		[][]int{{100}, {1000}, {420}, {500}},
	)

	txGas := uint64(100000)
	txs := make([]coretypes.Tx, 0, len(accounts)*2)
	txGasToSizeMap := make(map[float64]int)
	for i, acc := range accounts {
		signer, err := user.NewSigner(kr, enc, testutil.ChainID, user.NewAccount(acc, infos[i].AccountNum, infos[i].Sequence))
		require.NoError(t, err)
		bTxFee := rand.Uint64() % 10000
		bTx, _, err := signer.CreatePayForBlobs(acc, blobs[i],
			user.SetFee(bTxFee),
			user.SetGasLimit(txGas))
		require.NoError(t, err)
		bTxGasPrice := float64(bTxFee) / float64(txGas)

		sTxFee := rand.Uint64() % 10000
		sendTx := testutil.SendTxWithManualSequence(
			t,
			enc,
			kr,
			accounts[i],
			accounts[0],
			1000,
			testutil.ChainID,
			infos[i].Sequence,
			infos[i].AccountNum,
			user.SetFee(sTxFee),
			user.SetGasLimit(txGas),
		)
		sTxGasPrice := float64(sTxFee) / float64(txGas)

		txs = append(txs, sendTx)
		txs = append(txs, bTx)

		txGasToSizeMap[bTxGasPrice] = len(bTx)
		txGasToSizeMap[sTxGasPrice] = len(sendTx)
	}

	maxBytes := 3000
	gasPrices, err := gasestimation.SortAndExtractGasPrices(testApp.GetTxConfig().TxDecoder(), txs, int64(maxBytes))
	require.NoError(t, err)
	require.Greater(t, len(gasPrices), 0)

	currentGasPrice := gasPrices[0]
	currentSize := txGasToSizeMap[currentGasPrice]
	for _, gasPrice := range gasPrices[1:] {
		assert.GreaterOrEqual(t, gasPrice, currentGasPrice)
		currentSize += txGasToSizeMap[gasPrice]
	}
	assert.LessOrEqual(t, currentSize, maxBytes)
}

func TestEstimateGasPrice(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping app/test/gas_estimation gas price and usage in short mode.")
	}

	// test setup: create a test chain, submit a few PFBs to it, keep track of their gas
	// price, then test the gas estimator API.
	accountNames := testfactory.GenerateAccounts(10)
	cfg := testnode.DefaultConfig().WithFundedAccounts(accountNames...).
		WithDelayedPrecommitTimeout(10 * time.Second) // to have all transactions in the mempool without being included in a block

	cctx, _, _ := testnode.NewNetwork(t, cfg)

	err := cctx.WaitForNextBlock()
	require.NoError(t, err)

	// create the gas estimation client
	gasEstimationAPI := gasestimation.NewGasEstimatorClient(cctx.GRPCClient)

	// test getting the gas price for an empty blockchain
	resp, err := gasEstimationAPI.EstimateGasPrice(cctx.GoContext(), &gasestimation.EstimateGasPriceRequest{})
	require.NoError(t, err)
	assert.Equal(t, appconsts.DefaultMinGasPrice, resp.EstimatedGasPrice)

	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	txClient, err := user.SetupTxClient(cctx.GoContext(), cctx.Keyring, cctx.GRPCClient, enc)
	require.NoError(t, err)

	blobSize := (appconsts.DefaultMaxBytes - 1) / len(accountNames)
	msg, err := blobtypes.NewMsgPayForBlobs(accountNames[0], 0, blobfactory.ManyBlobs(random.New(), []share.Namespace{share.RandomBlobNamespace()}, []int{blobSize})...)
	require.NoError(t, err)
	gasLimit := blobtypes.DefaultEstimateGas(msg)
	wg := &sync.WaitGroup{}
	for _, accName := range accountNames {
		wg.Go(func() {
			// ensure that it is greater than the min gas price
			gasPrice := float64(rand.Intn(1000)+1) * appconsts.DefaultMinGasPrice
			blobs := blobfactory.ManyBlobs(random.New(), []share.Namespace{share.RandomBlobNamespace()}, []int{blobSize})
			resp, err := txClient.BroadcastPayForBlobWithAccount(
				cctx.GoContext(),
				accName,
				blobs,
				user.SetGasLimitAndGasPrice(gasLimit, gasPrice),
			)
			require.NoError(t, err)
			require.Equal(t, abci.CodeTypeOK, resp.Code, resp.RawLog)
		})
	}
	wg.Wait()

	// Query the gas estimation API for each priority level.
	getLowResp, err := gasEstimationAPI.EstimateGasPrice(cctx.GoContext(), &gasestimation.EstimateGasPriceRequest{TxPriority: gasestimation.TxPriority_TX_PRIORITY_LOW})
	require.NoError(t, err)
	getMediumResp, err := gasEstimationAPI.EstimateGasPrice(cctx.GoContext(), &gasestimation.EstimateGasPriceRequest{TxPriority: gasestimation.TxPriority_TX_PRIORITY_MEDIUM})
	require.NoError(t, err)
	getHighResp, err := gasEstimationAPI.EstimateGasPrice(cctx.GoContext(), &gasestimation.EstimateGasPriceRequest{TxPriority: gasestimation.TxPriority_TX_PRIORITY_HIGH})
	require.NoError(t, err)
	getNoneResp, err := gasEstimationAPI.EstimateGasPrice(cctx.GoContext(), &gasestimation.EstimateGasPriceRequest{TxPriority: gasestimation.TxPriority_TX_PRIORITY_UNSPECIFIED})
	require.NoError(t, err)

	low := getLowResp.EstimatedGasPrice
	medium := getMediumResp.EstimatedGasPrice
	high := getHighResp.EstimatedGasPrice
	none := getNoneResp.EstimatedGasPrice

	// Assert the relative ordering of gas price estimates: LOW <= MEDIUM <= HIGH.
	assert.LessOrEqual(t, low, medium, "LOW gas price should be <= MEDIUM")
	assert.LessOrEqual(t, medium, high, "MEDIUM gas price should be <= HIGH")
	// UNSPECIFIED should default to MEDIUM.
	assert.Equal(t, medium, none, "UNSPECIFIED should return the same gas price as MEDIUM")
	// All estimates should be at least the minimum gas price.
	assert.GreaterOrEqual(t, low, appconsts.DefaultMinGasPrice, "LOW gas price should be >= min gas price")
}

func TestEstimateGasUsed(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping app/test/gas_estimation gas price and usage in short mode.")
	}

	cfg := testnode.DefaultConfig().WithFundedAccounts("test")
	cctx, _, _ := testnode.NewNetwork(t, cfg)
	require.NoError(t, cctx.WaitForNextBlock())

	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	txClient, err := user.SetupTxClient(cctx.GoContext(), cctx.Keyring, cctx.GRPCClient, enc)
	require.NoError(t, err)
	addr := testfactory.GetAddress(cctx.Keyring, "test")

	// create a transfer transaction
	msg := banktypes.NewMsgSend(
		addr,
		testnode.RandomAddress().(sdk.AccAddress),
		sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, 10)),
	)
	rawTx, _, err := txClient.Signer().CreateTx(
		[]sdk.Msg{msg},
		user.SetGasLimit(0), // set to 0 to mimic txClient behavior
		user.SetFee(1),
	)
	require.NoError(t, err)

	gasEstimationAPI := gasestimation.NewGasEstimatorClient(cctx.GRPCClient)

	// calculate the expected gas used
	_, expectedGasEstimate, err := txClient.EstimateGasPriceAndUsage(cctx.GoContext(), []sdk.Msg{msg}, gasestimation.TxPriority_TX_PRIORITY_MEDIUM)
	require.NoError(t, err)
	// calculate the actual gas used
	actualGasEstimate, err := gasEstimationAPI.EstimateGasPriceAndUsage(cctx.GoContext(), &gasestimation.EstimateGasPriceAndUsageRequest{TxBytes: rawTx})
	require.NoError(t, err)

	assert.Equal(t, expectedGasEstimate, actualGasEstimate.EstimatedGasUsed)

	// create a PFB
	blobSize := 100
	blobs := blobfactory.ManyRandBlobs(random.New(), blobSize)
	pfbTx, _, err := txClient.Signer().CreatePayForBlobs(
		"test",
		blobs,
		user.SetGasLimit(0), // set to 0 to mimic txClient behavior
		user.SetFee(1),
	)
	require.NoError(t, err)
	pfbMsg, err := blobtypes.NewMsgPayForBlobs(addr.String(), appconsts.Version, blobs...)
	require.NoError(t, err)

	// calculate the expected gas used
	_, expectedGasEstimate, err = txClient.EstimateGasPriceAndUsage(cctx.GoContext(), []sdk.Msg{pfbMsg}, gasestimation.TxPriority_TX_PRIORITY_MEDIUM)
	require.NoError(t, err)
	// calculate the actual gas used
	actualGasEstimate, err = gasEstimationAPI.EstimateGasPriceAndUsage(cctx.GoContext(), &gasestimation.EstimateGasPriceAndUsageRequest{TxBytes: pfbTx})
	require.NoError(t, err)

	assert.Equal(t, expectedGasEstimate, actualGasEstimate.EstimatedGasUsed)
}
