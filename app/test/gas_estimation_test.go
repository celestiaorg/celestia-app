package app

import (
	"fmt"
	"github.com/celestiaorg/celestia-app/v3/app"
	"github.com/celestiaorg/celestia-app/v3/app/encoding"
	"github.com/celestiaorg/celestia-app/v3/app/grpc/gas_estimation"
	"github.com/celestiaorg/celestia-app/v3/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v3/pkg/user"
	"github.com/celestiaorg/celestia-app/v3/test/util/blobfactory"
	"github.com/celestiaorg/celestia-app/v3/test/util/testfactory"
	"github.com/celestiaorg/celestia-app/v3/test/util/testnode"
	blobtypes "github.com/celestiaorg/celestia-app/v3/x/blob/types"
	"github.com/celestiaorg/go-square/v2/share"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	abci "github.com/tendermint/tendermint/abci/types"
	tmrand "github.com/tendermint/tendermint/libs/rand"
	"sync"
	"testing"
	"time"
)

func TestEstimateGasPrice(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping app/test/gas_estimation gas price and usage in short mode.")
	}

	// test setup: create a test chain, submit a few PFBs to it, keep track of their gas
	// price, then test the gas estimator API.
	accountNames := testfactory.GenerateAccounts(150) // using 150 to have 2 pages of txs
	cfg := testnode.DefaultConfig().WithFundedAccounts(accountNames...).
		WithTimeoutCommit(10 * time.Second) // to have all the transactions in just a few blocks
	cctx, _, _ := testnode.NewNetwork(t, cfg)
	require.NoError(t, cctx.WaitForNextBlock())

	encfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	rand := tmrand.NewRand()

	var err error
	txClient, err := user.SetupTxClient(cctx.GoContext(), cctx.Keyring, cctx.GRPCClient, encfg)
	require.NoError(t, err)

	gasLimit := blobtypes.DefaultEstimateGas([]uint32{100})
	gasPricesChan := make(chan float64, len(accountNames))
	wg := &sync.WaitGroup{}
	for _, accName := range accountNames {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// ensure that it is greater than the min gas price
			gasPrice := float64(rand.Intn(1000)+1) * appconsts.DefaultMinGasPrice
			blobs := blobfactory.ManyBlobs(rand, []share.Namespace{share.RandomBlobNamespace()}, []int{100})
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
	err = cctx.WaitForNextBlock()
	require.NoError(t, err)

	close(gasPricesChan)
	gasPrices := make([]float64, 0, len(accountNames))
	for price := range gasPricesChan {
		gasPrices = append(gasPrices, price)
	}

	// create the gas estimation client
	gasEstimationAPI := gas_estimation.NewGasEstimatorClient(cctx.GRPCClient)
	meanGasPrice := gas_estimation.Mean(gasPrices)
	stDev := gas_estimation.StandardDeviation(meanGasPrice, gasPrices)
	tests := []struct {
		name             string
		priority         gas_estimation.TxPriority
		expectedGasPrice float64
	}{
		{
			name:     "NONE -> same as MEDIUM (mean)",
			priority: gas_estimation.TxPriority_NONE,
			expectedGasPrice: func() float64 {
				return meanGasPrice
			}(),
		},
		{
			name:     "LOW -> mean - ZScore * stDev",
			priority: gas_estimation.TxPriority_LOW,
			expectedGasPrice: func() float64 {
				return meanGasPrice - gas_estimation.EstimationZScore*stDev
			}(),
		},
		{
			name:     "MEDIUM -> mean",
			priority: gas_estimation.TxPriority_MEDIUM,
			expectedGasPrice: func() float64 {
				return meanGasPrice
			}(),
		},
		{
			name:     "HIGH -> mean + ZScore * stDev",
			priority: gas_estimation.TxPriority_HIGH,
			expectedGasPrice: func() float64 {
				return meanGasPrice + gas_estimation.EstimationZScore*stDev
			}(),
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			resp, err := gasEstimationAPI.EstimateGasPrice(cctx.GoContext(), &gas_estimation.EstimateGasPriceRequest{TxPriority: tt.priority})
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

	blobSize := 100
	blobs := blobfactory.ManyRandBlobs(tmrand.NewRand(), blobSize)
	pfbTx, _, err := txClient.Signer().CreatePayForBlobs("test", blobs)

	expectedGasEstimate := blobtypes.DefaultEstimateGas([]uint32{uint32(blobSize)})

	gasEstimationAPI := gas_estimation.NewGasEstimatorClient(cctx.GRPCClient)
	actualGasEstimate, err := gasEstimationAPI.EstimateGasPriceAndUsage(cctx.GoContext(), &gas_estimation.EstimateGasPriceAndUsageRequest{TxBytes: pfbTx})
	require.NoError(t, err)

	assert.Equal(t, expectedGasEstimate, actualGasEstimate.EstimatedGasUsed)
}
