package gasestimation_test

import (
	"context"
	"math"
	"math/rand"
	"testing"

	"github.com/celestiaorg/celestia-app/v5/app"
	"github.com/celestiaorg/celestia-app/v5/app/encoding"
	"github.com/celestiaorg/celestia-app/v5/app/grpc/gasestimation"
	"github.com/celestiaorg/celestia-app/v5/test/util/blobfactory"
	"github.com/celestiaorg/celestia-app/v5/test/util/random"
	"github.com/celestiaorg/celestia-app/v5/test/util/testnode"
	rpctypes "github.com/cometbft/cometbft/rpc/core/types"
	"github.com/cometbft/cometbft/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockMempoolClient implements client.MempoolClient for testing
type mockMempoolClient struct {
	unconfirmedTxs *rpctypes.ResultUnconfirmedTxs
	err            error
}

func (m *mockMempoolClient) UnconfirmedTxs(ctx context.Context, limit *int) (*rpctypes.ResultUnconfirmedTxs, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.unconfirmedTxs, nil
}

func (m *mockMempoolClient) NumUnconfirmedTxs(ctx context.Context) (*rpctypes.ResultUnconfirmedTxs, error) {
	return nil, nil
}

func (m *mockMempoolClient) CheckTx(ctx context.Context, tx types.Tx) (*rpctypes.ResultCheckTx, error) {
	return nil, nil
}

// mockGovMaxSquareBytesFn returns a fixed value for testing
func mockGovMaxSquareBytesFn() (uint64, error) {
	return 1000000, nil // 1MB
}

// mockSimulateFn simulates gas usage for testing
func mockSimulateFn(txBytes []byte) (sdk.GasInfo, *sdk.Result, error) {
	// Return a fixed gas usage for testing
	return sdk.GasInfo{
		GasUsed:   100000,
		GasWanted: 100000,
	}, &sdk.Result{}, nil
}

func TestGasEstimatorServer_EstimateGasPrice_EmptyMempool(t *testing.T) {
	// Test that when mempool is empty, local min gas price is returned
	localMinGasPrice := 0.005

	mockClient := &mockMempoolClient{
		unconfirmedTxs: &rpctypes.ResultUnconfirmedTxs{
			Txs:        []types.Tx{},
			Total:      0,
			TotalBytes: 0,
		},
	}

	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	server := gasestimation.NewGasEstimatorServer(
		mockClient,
		encCfg.TxConfig.TxDecoder(),
		mockGovMaxSquareBytesFn,
		mockSimulateFn,
		localMinGasPrice,
	)

	ctx := context.Background()
	request := &gasestimation.EstimateGasPriceRequest{
		TxPriority: gasestimation.TxPriority_TX_PRIORITY_MEDIUM,
	}

	response, err := server.EstimateGasPrice(ctx, request)
	require.NoError(t, err)
	assert.Equal(t, localMinGasPrice, response.EstimatedGasPrice)
}

func TestGasEstimatorServer_EstimateGasPrice_MempoolNotFull(t *testing.T) {
	// Test that when mempool is not full enough (less than 70% threshold), local min gas price is returned
	localMinGasPrice := 0.005
	govMaxSquareBytes := uint64(1000000) // 1MB
	threshold := 0.70
	mempoolBytes := uint64(float64(govMaxSquareBytes) * threshold * 0.5) // 35% of max

	mockClient := &mockMempoolClient{
		unconfirmedTxs: &rpctypes.ResultUnconfirmedTxs{
			Txs:        []types.Tx{},
			Total:      0,
			TotalBytes: int64(mempoolBytes),
		},
	}

	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	server := gasestimation.NewGasEstimatorServer(
		mockClient,
		encCfg.TxConfig.TxDecoder(),
		mockGovMaxSquareBytesFn,
		mockSimulateFn,
		localMinGasPrice,
	)

	ctx := context.Background()
	request := &gasestimation.EstimateGasPriceRequest{
		TxPriority: gasestimation.TxPriority_TX_PRIORITY_MEDIUM,
	}

	response, err := server.EstimateGasPrice(ctx, request)
	require.NoError(t, err)
	assert.Equal(t, localMinGasPrice, response.EstimatedGasPrice)
}

func TestGasEstimatorServer_EstimateGasPrice_MempoolFull(t *testing.T) {
	// Test that when mempool is full enough, gas price is estimated from transactions
	localMinGasPrice := 0.005
	govMaxSquareBytes := uint64(1000000) // 1MB
	threshold := 0.70
	mempoolBytes := uint64(float64(govMaxSquareBytes) * threshold * 1.2) // 84% of max

	signer, err := testnode.NewOfflineSigner()
	require.NoError(t, err)

	mockTxs := blobfactory.RandMultiBlobTxsSameSigner(t, rand.New(rand.NewSource(0)), signer, 3)

	mockClient := &mockMempoolClient{
		unconfirmedTxs: &rpctypes.ResultUnconfirmedTxs{
			Txs:        mockTxs,
			Total:      len(mockTxs),
			TotalBytes: int64(mempoolBytes),
		},
	}

	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	server := gasestimation.NewGasEstimatorServer(
		mockClient,
		encCfg.TxConfig.TxDecoder(),
		mockGovMaxSquareBytesFn,
		mockSimulateFn,
		localMinGasPrice,
	)

	ctx := context.Background()
	request := &gasestimation.EstimateGasPriceRequest{
		TxPriority: gasestimation.TxPriority_TX_PRIORITY_MEDIUM,
	}

	response, err := server.EstimateGasPrice(ctx, request)
	require.NoError(t, err)
	// Should return a gas price different from local min when mempool is full
	assert.NotEqual(t, localMinGasPrice, response.EstimatedGasPrice)
	assert.Greater(t, response.EstimatedGasPrice, 0.0)
}

func TestGasEstimatorServer_EstimateGasPriceAndUsage(t *testing.T) {
	localMinGasPrice := 0.005

	mockClient := &mockMempoolClient{
		unconfirmedTxs: &rpctypes.ResultUnconfirmedTxs{
			Txs:        []types.Tx{},
			Total:      0,
			TotalBytes: 0,
		},
	}

	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	server := gasestimation.NewGasEstimatorServer(
		mockClient,
		encCfg.TxConfig.TxDecoder(),
		mockGovMaxSquareBytesFn,
		mockSimulateFn,
		localMinGasPrice,
	)

	ctx := context.Background()

	// Test with normal transaction bytes
	txBytes := []byte("mock_transaction_bytes")
	request := &gasestimation.EstimateGasPriceAndUsageRequest{
		TxBytes:    txBytes,
		TxPriority: gasestimation.TxPriority_TX_PRIORITY_MEDIUM,
	}

	response, err := server.EstimateGasPriceAndUsage(ctx, request)
	require.NoError(t, err)

	// Check gas price
	assert.Equal(t, localMinGasPrice, response.EstimatedGasPrice)

	// Check gas usage (should be 100000 * 1.1 = 110000 from mockSimulateFn)
	expectedGasUsed := uint64(math.Round(100000 * gasestimation.GasMultiplier))
	assert.Equal(t, expectedGasUsed, response.EstimatedGasUsed)
}

func TestGasEstimatorServer_EstimateGasPriceAndUsage_BlobTx(t *testing.T) {
	localMinGasPrice := 0.005

	mockClient := &mockMempoolClient{
		unconfirmedTxs: &rpctypes.ResultUnconfirmedTxs{
			Txs:        []types.Tx{},
			Total:      0,
			TotalBytes: 0,
		},
	}

	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	server := gasestimation.NewGasEstimatorServer(
		mockClient,
		encCfg.TxConfig.TxDecoder(),
		mockGovMaxSquareBytesFn,
		mockSimulateFn,
		localMinGasPrice,
	)

	request := &gasestimation.EstimateGasPriceAndUsageRequest{
		TxBytes:    random.Bytes(100),
		TxPriority: gasestimation.TxPriority_TX_PRIORITY_MEDIUM,
	}

	ctx := context.Background()
	response, err := server.EstimateGasPriceAndUsage(ctx, request)
	require.NoError(t, err)

	// Check gas price
	assert.Equal(t, localMinGasPrice, response.EstimatedGasPrice)

	// Check gas usage (should be 100000 * 1.1 = 110000 from mockSimulateFn)
	expectedGasUsed := uint64(math.Round(100000 * gasestimation.GasMultiplier))
	assert.Equal(t, expectedGasUsed, response.EstimatedGasUsed)
}

func TestGasEstimatorServer_EstimateGasPrice_DifferentPriorities(t *testing.T) {
	localMinGasPrice := 0.005
	govMaxSquareBytes := uint64(1000000) // 1MB
	threshold := 0.70
	mempoolBytes := uint64(float64(govMaxSquareBytes) * threshold * 1.2) // 84% of max

	signer, err := testnode.NewOfflineSigner()
	require.NoError(t, err)

	mockTxs := blobfactory.RandMultiBlobTxsSameSigner(t, rand.New(rand.NewSource(0)), signer, 5)

	mockClient := &mockMempoolClient{
		unconfirmedTxs: &rpctypes.ResultUnconfirmedTxs{
			Txs:        mockTxs,
			Total:      len(mockTxs),
			TotalBytes: int64(mempoolBytes),
		},
	}

	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	server := gasestimation.NewGasEstimatorServer(
		mockClient,
		encCfg.TxConfig.TxDecoder(),
		mockGovMaxSquareBytesFn,
		mockSimulateFn,
		localMinGasPrice,
	)

	ctx := context.Background()

	// Test different priorities
	priorities := []gasestimation.TxPriority{
		gasestimation.TxPriority_TX_PRIORITY_LOW,
		gasestimation.TxPriority_TX_PRIORITY_MEDIUM,
		gasestimation.TxPriority_TX_PRIORITY_HIGH,
		gasestimation.TxPriority_TX_PRIORITY_UNSPECIFIED,
	}

	for _, priority := range priorities {
		t.Run(priority.String(), func(t *testing.T) {
			request := &gasestimation.EstimateGasPriceRequest{
				TxPriority: priority,
			}

			response, err := server.EstimateGasPrice(ctx, request)
			require.NoError(t, err)
			assert.True(t, response.EstimatedGasPrice > 0.0)
		})
	}
}

func TestGasEstimatorServer_EstimateGasPrice_MempoolError(t *testing.T) {
	localMinGasPrice := 0.005

	mockClient := &mockMempoolClient{
		err: assert.AnError,
	}

	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	server := gasestimation.NewGasEstimatorServer(
		mockClient,
		encCfg.TxConfig.TxDecoder(),
		mockGovMaxSquareBytesFn,
		mockSimulateFn,
		localMinGasPrice,
	)

	ctx := context.Background()
	request := &gasestimation.EstimateGasPriceRequest{
		TxPriority: gasestimation.TxPriority_TX_PRIORITY_MEDIUM,
	}

	response, err := server.EstimateGasPrice(ctx, request)
	assert.Error(t, err)
	assert.Nil(t, response)
}

func TestGasEstimatorServer_EstimateGasPriceAndUsage_SimulationError(t *testing.T) {
	localMinGasPrice := 0.005

	mockClient := &mockMempoolClient{
		unconfirmedTxs: &rpctypes.ResultUnconfirmedTxs{
			Txs:        []types.Tx{},
			Total:      0,
			TotalBytes: 0,
		},
	}

	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	// Create a server with a mock simulate function that returns an error
	server := gasestimation.NewGasEstimatorServer(
		mockClient,
		encCfg.TxConfig.TxDecoder(),
		mockGovMaxSquareBytesFn,
		func(txBytes []byte) (sdk.GasInfo, *sdk.Result, error) {
			return sdk.GasInfo{}, nil, assert.AnError
		},
		localMinGasPrice,
	)

	ctx := context.Background()
	txBytes := []byte("mock_transaction_bytes")
	request := &gasestimation.EstimateGasPriceAndUsageRequest{
		TxBytes:    txBytes,
		TxPriority: gasestimation.TxPriority_TX_PRIORITY_MEDIUM,
	}

	response, err := server.EstimateGasPriceAndUsage(ctx, request)
	assert.Error(t, err)
	assert.Nil(t, response)
}
