package gasestimation

import (
	"math"
	"testing"

	"github.com/celestiaorg/celestia-app/v3/pkg/appconsts"
	"github.com/tendermint/tendermint/libs/rand"

	"github.com/stretchr/testify/require"

	"github.com/stretchr/testify/assert"
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
		randomGasPrice := minGasPrice + rand.Float64()*(maxGasPrice-minGasPrice)
		gasPrices[i] = randomGasPrice
	}
	medianGasPrice, err := Median(gasPrices)
	require.NoError(t, err)
	medianGasPrice = medianGasPrice * mediumPriorityGasAdjustmentRate
	bottomMedian, err := Median(gasPrices[:len(gasPrices)*10/100])
	require.NoError(t, err)
	topMedian, err := Median(gasPrices[len(gasPrices)*90/100:])
	require.NoError(t, err)
	topMedian = topMedian * highPriorityGasAdjustmentRate

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
