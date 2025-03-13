package app_test

import (
	"fmt"
	"math/rand"
	"sort"
	"sync"
	"testing"
	"time"

	testutil "github.com/celestiaorg/celestia-app/v3/test/util"
	"github.com/tendermint/tendermint/types"

	"github.com/celestiaorg/celestia-app/v3/app"
	"github.com/celestiaorg/celestia-app/v3/app/encoding"
	"github.com/celestiaorg/celestia-app/v3/app/grpc/gasestimation"
	"github.com/celestiaorg/celestia-app/v3/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v3/pkg/user"
	"github.com/celestiaorg/celestia-app/v3/test/util/blobfactory"
	"github.com/celestiaorg/celestia-app/v3/test/util/testfactory"
	"github.com/celestiaorg/celestia-app/v3/test/util/testnode"
	blobtypes "github.com/celestiaorg/celestia-app/v3/x/blob/types"
	"github.com/celestiaorg/go-square/v2/share"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	abci "github.com/tendermint/tendermint/abci/types"
	tmrand "github.com/tendermint/tendermint/libs/rand"
)

func TestSortAndExtractGasPrice(t *testing.T) {
	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...).TxConfig
	accounts := testfactory.GenerateAccounts(4)
	testApp, kr := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams(), accounts...)
	infos := queryAccountInfo(testApp, accounts, kr)
	blobs := blobfactory.NestedBlobs(
		t,
		testfactory.RandomBlobNamespaces(tmrand.NewRand(), 4),
		[][]int{{100}, {1000}, {420}, {500}},
	)

	txGas := uint64(100000)
	txs := make([]types.Tx, 0)
	txGasToSizeMap := make(map[float64]int)
	for i, acc := range accounts {
		signer, err := user.NewSigner(kr, enc, testutil.ChainID, appconsts.LatestVersion, user.NewAccount(acc, infos[i].AccountNum, infos[i].Sequence))
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
		WithTimeoutCommit(200 * time.Second) // to have all transactions in the mempool without being included in a block
	cctx, _, _ := testnode.NewNetwork(t, cfg)

	// create the gas estimation client
	gasEstimationAPI := gasestimation.NewGasEstimatorClient(cctx.GRPCClient)

	// test getting the gas price for an empty blockchain
	resp, err := gasEstimationAPI.EstimateGasPrice(cctx.GoContext(), &gasestimation.EstimateGasPriceRequest{})
	require.NoError(t, err)
	assert.Equal(t, appconsts.DefaultMinGasPrice, resp.EstimatedGasPrice)

	encfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	rand := tmrand.NewRand()

	txClient, err := user.SetupTxClient(cctx.GoContext(), cctx.Keyring, cctx.GRPCClient, encfg)
	require.NoError(t, err)

	blobSize := (appconsts.DefaultMaxBytes - 1) / len(accountNames)
	gasLimit := blobtypes.DefaultEstimateGas([]uint32{uint32(blobSize)})
	wg := &sync.WaitGroup{}
	gasPricesChan := make(chan float64, len(accountNames))
	for _, accName := range accountNames {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// ensure that it is greater than the min gas price
			gasPrice := float64(rand.Intn(1000)+1) * appconsts.DefaultMinGasPrice
			blobs := blobfactory.ManyBlobs(rand, []share.Namespace{share.RandomBlobNamespace()}, []int{blobSize})

			resp, err := txClient.BroadcastPayForBlobWithAccount(
				cctx.GoContext(),
				accName,
				blobs,
				user.SetGasLimitAndGasPrice(gasLimit, gasPrice),
			)
			require.NoError(t, err)
			require.Equal(t, abci.CodeTypeOK, resp.Code, resp.RawLog)
			gasPricesChan <- gasPrice
		}()
	}
	wg.Wait()

	close(gasPricesChan)
	gasPrices := make([]float64, 0, len(accountNames))
	for price := range gasPricesChan {
		gasPrices = append(gasPrices, price)
	}
	sort.Float64s(gasPrices)

	medianGasPrices, err := gasestimation.Median(gasPrices)
	require.NoError(t, err)
	bottomMedian, err := gasestimation.Median(gasPrices[:len(gasPrices)*10/100])
	require.NoError(t, err)
	topMedian, err := gasestimation.Median(gasPrices[len(gasPrices)*90/100:])
	require.NoError(t, err)

	tests := []struct {
		name             string
		priority         gasestimation.TxPriority
		expectedGasPrice float64
	}{
		{
			name:             "NONE -> same as MEDIUM (median)",
			priority:         gasestimation.TxPriority_TX_PRIORITY_UNSPECIFIED,
			expectedGasPrice: medianGasPrices,
		},
		{
			name:             "LOW -> bottom 10% median",
			priority:         gasestimation.TxPriority_TX_PRIORITY_LOW,
			expectedGasPrice: bottomMedian,
		},
		{
			name:             "MEDIUM -> median",
			priority:         gasestimation.TxPriority_TX_PRIORITY_MEDIUM,
			expectedGasPrice: medianGasPrices,
		},
		{
			name:             "HIGH -> top 10% median",
			priority:         gasestimation.TxPriority_TX_PRIORITY_HIGH,
			expectedGasPrice: topMedian,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := gasEstimationAPI.EstimateGasPrice(cctx.GoContext(), &gasestimation.EstimateGasPriceRequest{TxPriority: tt.priority})
			require.NoError(t, err)
			assert.Equal(t, fmt.Sprintf("%.2f", tt.expectedGasPrice), fmt.Sprintf("%.2f", resp.EstimatedGasPrice))
		})
	}
}

func TestEstimateGasUsed(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping app/test/gas_estimation gas price and usage in short mode.")
	}

	cfg := testnode.DefaultConfig().WithFundedAccounts("test")
	cctx, _, _ := testnode.NewNetwork(t, cfg)
	require.NoError(t, cctx.WaitForNextBlock())

	encfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	txClient, err := user.SetupTxClient(cctx.GoContext(), cctx.Keyring, cctx.GRPCClient, encfg)
	require.NoError(t, err)
	txClient.SetGasMultiplier(1)
	addr := testfactory.GetAddress(cctx.Keyring, "test")

	// create a transfer transaction
	msg := banktypes.NewMsgSend(
		addr,
		testnode.RandomAddress().(sdk.AccAddress),
		sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, 10)),
	)
	rawTx, err := txClient.Signer().CreateTx(
		[]sdk.Msg{msg},
		user.SetGasLimit(0), // set to 0 to mimic txClient behavior
		user.SetFee(1),
	)
	require.NoError(t, err)

	gasEstimationAPI := gasestimation.NewGasEstimatorClient(cctx.GRPCClient)

	// calculate the expected gas used
	expectedGasEstimate, err := txClient.EstimateGas(cctx.GoContext(), []sdk.Msg{msg})
	require.NoError(t, err)
	// calculate the actual gas used
	actualGasEstimate, err := gasEstimationAPI.EstimateGasPriceAndUsage(cctx.GoContext(), &gasestimation.EstimateGasPriceAndUsageRequest{TxBytes: rawTx})
	require.NoError(t, err)

	assert.Equal(t, expectedGasEstimate, actualGasEstimate.EstimatedGasUsed)

	// create a PFB
	blobSize := 100
	blobs := blobfactory.ManyRandBlobs(tmrand.NewRand(), blobSize)
	pfbTx, _, err := txClient.Signer().CreatePayForBlobs(
		"test",
		blobs,
		user.SetGasLimit(0), // set to 0 to mimic txClient behavior
		user.SetFee(1),
	)
	require.NoError(t, err)
	pfbMsg, err := blobtypes.NewMsgPayForBlobs(addr.String(), appconsts.LatestVersion, blobs...)
	require.NoError(t, err)

	// calculate the expected gas used
	expectedGasEstimate, err = txClient.EstimateGas(cctx.GoContext(), []sdk.Msg{pfbMsg})
	require.NoError(t, err)
	// calculate the actual gas used
	actualGasEstimate, err = gasEstimationAPI.EstimateGasPriceAndUsage(cctx.GoContext(), &gasestimation.EstimateGasPriceAndUsageRequest{TxBytes: pfbTx})
	require.NoError(t, err)

	assert.Equal(t, expectedGasEstimate, actualGasEstimate.EstimatedGasUsed)
}
