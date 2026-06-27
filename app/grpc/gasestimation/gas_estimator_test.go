package gasestimation

import (
	"context"
	"encoding/binary"
	"errors"
	"math"
	"testing"

	"github.com/celestiaorg/celestia-app/v9/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v9/test/util/random"
	rpctypes "github.com/cometbft/cometbft/rpc/core/types"
	"github.com/cometbft/cometbft/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	protov2 "google.golang.org/protobuf/proto"
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

// TestEstimateGasPriceLimitsToGovMaxBytes verifies that estimateGasPrice only
// considers the transactions that would actually fit within the governance max
// block size (gov max square bytes) rather than the larger upper bound on max
// bytes. When the mempool contains a small set of high gas price transactions
// that fill a gov-sized block plus many cheaper transactions that would only
// fit in a larger block, the estimate must reflect the high gas price
// transactions that would actually be included.
func TestEstimateGasPriceLimitsToGovMaxBytes(t *testing.T) {
	const txSize = 100
	// govMaxSquareBytes only has room for the high gas price transactions.
	const govMaxSquareBytes = 10 * txSize

	decoder := newMockTxDecoder()
	txs := make([]types.Tx, 0, 110)
	// 10 high gas price transactions with prices spread across 100..109 so the
	// values aren't tightly clustered (which would trigger a priority
	// adjustment). The median of these prices is 104.5.
	for i := range 10 {
		txs = append(txs, decoder.add(txSize, int64(100+i), 1))
	}
	// 100 low gas price transactions: gas price = 1. These only fit when the
	// larger upper bound on max bytes is (incorrectly) used.
	for range 100 {
		txs = append(txs, decoder.add(txSize, 1, 1))
	}

	server := &gasEstimatorServer{
		mempoolClient: newMockMempoolClient(txs),
		txDecoder:     decoder.decode,
		minGasPriceFn: func() (float64, error) { return 0, nil },
		govMaxSquareBytesFn: func() (uint64, error) {
			return govMaxSquareBytes, nil
		},
	}

	gasPrice, err := server.estimateGasPrice(context.Background(), TxPriority_TX_PRIORITY_MEDIUM)
	require.NoError(t, err)
	// Only the high gas price transactions fit in a gov-sized block, so the
	// estimate should be their median (104.5), not the cheaper transactions'
	// price of 1.
	require.Equal(t, 104.5, gasPrice)
}

// mockTxDecoder builds mock transactions of a chosen size and decodes them back
// into mockFeeTx values carrying a fee and gas.
type mockTxDecoder struct {
	feeTxs map[string]mockFeeTx
	next   uint32
}

func newMockTxDecoder() *mockTxDecoder {
	return &mockTxDecoder{feeTxs: make(map[string]mockFeeTx)}
}

// add returns a transaction of the requested size whose decoded form reports
// the provided fee and gas.
func (d *mockTxDecoder) add(size int, fee, gas int64) types.Tx {
	raw := make([]byte, size)
	// Make each transaction unique so the decoder map keys don't collide.
	binary.BigEndian.PutUint32(raw, d.next)
	d.next++
	d.feeTxs[string(raw)] = mockFeeTx{
		fee: sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, fee)),
		gas: uint64(gas),
	}
	return raw
}

func (d *mockTxDecoder) decode(txBytes []byte) (sdk.Tx, error) {
	feeTx, ok := d.feeTxs[string(txBytes)]
	if !ok {
		return nil, errors.New("unknown transaction")
	}
	return feeTx, nil
}

// mockFeeTx is a minimal sdk.FeeTx used to exercise gas price extraction.
type mockFeeTx struct {
	fee sdk.Coins
	gas uint64
}

func (m mockFeeTx) GetGas() uint64                        { return m.gas }
func (m mockFeeTx) GetFee() sdk.Coins                     { return m.fee }
func (m mockFeeTx) FeePayer() []byte                      { return nil }
func (m mockFeeTx) FeeGranter() []byte                    { return nil }
func (m mockFeeTx) GetMsgs() []sdk.Msg                    { return nil }
func (m mockFeeTx) GetMsgsV2() ([]protov2.Message, error) { return nil, nil }

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
