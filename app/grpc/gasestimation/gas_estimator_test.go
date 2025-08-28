package gasestimation

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/celestiaorg/celestia-app/v6/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v6/test/util/random"
	rpctypes "github.com/cometbft/cometbft/rpc/core/types"
	"github.com/cometbft/cometbft/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMedian(t *testing.T) {
	tests := []struct {
		name      string
		gasPrices []float64
		want      float64
		wantErr   bool
	}{
		{
			name:      "Empty slice",
			gasPrices: []float64{},
			wantErr:   true,
		},
		{
			name:      "Single element",
			gasPrices: []float64{10},
			want:      10,
		},
		{
			name:      "Multiple elements",
			gasPrices: []float64{1, 2, 3, 4, 5},
			want:      3,
		},
		{
			name:      "Mixed floats",
			gasPrices: []float64{1.5, 2.5, 3.5},
			want:      2.5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Median(tt.gasPrices)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestEstimateGasPriceForTransactions(t *testing.T) {
	gasPrices := []float64{10, 20, 30, 40, 50, 60, 70, 80, 90, 100, 110}
	medianGasPrice, err := Median(gasPrices)
	require.NoError(t, err)
	bottomMedian, err := Median(gasPrices[:len(gasPrices)*10/100])
	require.NoError(t, err)
	topMedian, err := Median(gasPrices[len(gasPrices)*90/100:])
	require.NoError(t, err)

	tests := []struct {
		name     string
		priority TxPriority
		want     float64
		wantErr  bool
	}{
		{
			name:     "NONE -> same as MEDIUM (median)",
			priority: TxPriority_TX_PRIORITY_UNSPECIFIED,
			want:     medianGasPrice,
		},
		{
			name:     "LOW -> bottom 10% median",
			priority: TxPriority_TX_PRIORITY_LOW,
			want:     bottomMedian,
		},
		{
			name:     "MEDIUM -> median",
			priority: TxPriority_TX_PRIORITY_MEDIUM,
			want:     medianGasPrice,
		},
		{
			name:     "HIGH -> top 10% median",
			priority: TxPriority_TX_PRIORITY_HIGH,
			want:     topMedian,
		},
		{
			name:     "Unknown -> error",
			priority: 999,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := estimateGasPriceForTransactions(gasPrices, tt.priority)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

func TestEstimateClusteredGasPrices(t *testing.T) {
	minGasPrice := appconsts.DefaultMinGasPrice * 2
	maxGasPrice := minGasPrice + gasPriceAdjustmentThreshold
	gasPrices := make([]float64, 20)
	for i := range gasPrices {
		randomGasPrice := minGasPrice + random.Float64()*(maxGasPrice-minGasPrice)
		gasPrices[i] = randomGasPrice
	}
	medianGasPrice, err := Median(gasPrices)
	require.NoError(t, err)
	medianGasPrice *= mediumPriorityGasAdjustmentRate
	bottomMedian, err := Median(gasPrices[:len(gasPrices)*10/100])
	require.NoError(t, err)
	topMedian, err := Median(gasPrices[len(gasPrices)*90/100:])
	require.NoError(t, err)
	topMedian *= highPriorityGasAdjustmentRate

	tests := []struct {
		name     string
		priority TxPriority
		want     float64
		wantErr  bool
	}{
		{
			name:     "NONE -> same as MEDIUM (median)",
			priority: TxPriority_TX_PRIORITY_UNSPECIFIED,
			want:     medianGasPrice,
		},
		{
			name:     "LOW -> bottom 10% median",
			priority: TxPriority_TX_PRIORITY_LOW,
			want:     bottomMedian,
		},
		{
			name:     "MEDIUM -> median",
			priority: TxPriority_TX_PRIORITY_MEDIUM,
			want:     medianGasPrice,
		},
		{
			name:     "HIGH -> top 10% median",
			priority: TxPriority_TX_PRIORITY_HIGH,
			want:     topMedian,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := estimateGasPriceForTransactions(gasPrices, tt.priority)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

func TestEstimateGasPriceSmallList(t *testing.T) {
	gasPrices := []float64{10, 20, 30, 40, 50, 60}
	bottomMedian, err := Median(gasPrices[:1])
	require.NoError(t, err)

	got, err := estimateGasPriceForTransactions(gasPrices, TxPriority_TX_PRIORITY_LOW)
	assert.NoError(t, err)
	assert.Equal(t, got, bottomMedian)
}

func TestMean(t *testing.T) {
	tests := []struct {
		name      string
		gasPrices []float64
		want      float64
	}{
		{
			name:      "Empty slice",
			gasPrices: []float64{},
			want:      0,
		},
		{
			name:      "Single element",
			gasPrices: []float64{10},
			want:      10,
		},
		{
			name:      "Multiple elements",
			gasPrices: []float64{1, 2, 3, 4, 5},
			want:      3,
		},
		{
			name:      "Mixed floats",
			gasPrices: []float64{1.5, 2.5, 3.5},
			want:      2.5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Mean(tt.gasPrices)
			if got != tt.want {
				t.Errorf("mean(%v) = %v, want %v", tt.gasPrices, got, tt.want)
			}
		})
	}
}

func TestStandardDeviation(t *testing.T) {
	tests := []struct {
		name      string
		gasPrices []float64
		want      float64
	}{
		{
			name:      "Empty slice",
			gasPrices: []float64{},
			want:      0,
		},
		{
			name:      "Single element",
			gasPrices: []float64{10},
			want:      0,
		},
		{
			name:      "Multiple elements",
			gasPrices: []float64{10, 20, 30, 40, 50},
			// Variance = 200, stdev ~ 14.142135...
			want: 14.142135623730951,
		},
		{
			name:      "Identical elements",
			gasPrices: []float64{5, 5, 5, 5},
			want:      0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			meanVal := Mean(tt.gasPrices)
			got := StandardDeviation(meanVal, tt.gasPrices)
			// We'll do a tolerance check for floating-point comparisons.
			if math.Abs(got-tt.want) > 1e-9 {
				t.Errorf("stdDev(%v) = %v, want %v", tt.gasPrices, got, tt.want)
			}
		})
	}
}

func TestGasEstimatorWithNetworkMinGasPrice(t *testing.T) {
	// Test that the gas estimator respects the network minimum gas price
	networkMinGasPrice := 0.01 // Higher than default min gas price

	// Test with empty mempool
	emptyMempool := newMockMempoolClient([]types.Tx{})

	server := &gasEstimatorServer{
		mempoolClient: emptyMempool,
		minGasPriceFn: func() (float64, error) {
			return networkMinGasPrice, nil
		},
		govMaxSquareBytesFn: func() (uint64, error) {
			return 1000000, nil
		},
	}

	// Test when mempool is empty (should return network min gas price)
	gasPrice, err := server.estimateGasPrice(context.Background(), TxPriority_TX_PRIORITY_MEDIUM)
	require.NoError(t, err)
	require.Equal(t, networkMinGasPrice, gasPrice)

	// Test when minGasPriceFn returns an error (should return default min gas price)
	serverWithError := &gasEstimatorServer{
		mempoolClient: emptyMempool,
		minGasPriceFn: func() (float64, error) {
			return 0, errors.New("min fee module unavailable")
		},
		govMaxSquareBytesFn: func() (uint64, error) {
			return 1000000, nil
		},
	}

	_, err = serverWithError.estimateGasPrice(context.Background(), TxPriority_TX_PRIORITY_MEDIUM)
	require.Error(t, err)
}

type mockMempoolClient struct {
	txs        []types.Tx
	totalBytes int64
}

func newMockMempoolClient(txs []types.Tx) *mockMempoolClient {
	totalBytes := int64(0)
	for _, tx := range txs {
		totalBytes += int64(len(tx))
	}
	return &mockMempoolClient{
		txs:        txs,
		totalBytes: totalBytes,
	}
}

func (m *mockMempoolClient) UnconfirmedTxs(ctx context.Context, limit *int) (*rpctypes.ResultUnconfirmedTxs, error) {
	return &rpctypes.ResultUnconfirmedTxs{
		Txs:        m.txs,
		Total:      len(m.txs),
		TotalBytes: m.totalBytes,
	}, nil
}

func (m *mockMempoolClient) NumUnconfirmedTxs(ctx context.Context) (*rpctypes.ResultUnconfirmedTxs, error) {
	return nil, nil
}

func (m *mockMempoolClient) CheckTx(ctx context.Context, tx types.Tx) (*rpctypes.ResultCheckTx, error) {
	return nil, nil
}
