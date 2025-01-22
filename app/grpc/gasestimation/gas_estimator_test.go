package gasestimation

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
)

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

func TestEstimateGasPriceForTransactions(t *testing.T) {
	gasPrices := []float64{10, 20, 30, 40, 50}
	meanGasPrices := Mean(gasPrices)
	stDev := StandardDeviation(meanGasPrices, gasPrices)

	tests := []struct {
		name     string
		priority TxPriority
		want     float64
		wantErr  bool
	}{
		{
			name:     "NONE -> same as MEDIUM (mean)",
			priority: TxPriority_TX_PRIORITY_UNSPECIFIED,
			want:     meanGasPrices,
		},
		{
			name:     "LOW -> mean - ZScore * stDev",
			priority: TxPriority_TX_PRIORITY_LOW,
			want:     meanGasPrices - EstimationZScore*stDev,
		},
		{
			name:     "MEDIUM -> mean",
			priority: TxPriority_TX_PRIORITY_MEDIUM,
			want:     meanGasPrices,
		},
		{
			name:     "HIGH -> mean + ZScore * stDev",
			priority: TxPriority_TX_PRIORITY_HIGH,
			want:     meanGasPrices + EstimationZScore*stDev,
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
			if (err != nil) != tt.wantErr {
				// If we expect an error, don't bother checking the numeric result.
				if tt.wantErr {
					assert.Error(t, err)
				} else {
					assert.NoError(t, err)
					assert.Equal(t, tt.want, got)
				}
			}
		})
	}
}
