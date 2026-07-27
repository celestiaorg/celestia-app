package types

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestParamsValidateReplayInvariant covers the relationship Params.Validate
// enforces between the payment promise retention window and the withdrawal
// delay. A retention window shorter than the withdrawal delay lets a settled
// promise be replayed once its processed payment record is pruned; see
// TestRetentionWindowShorterThanWithdrawalDelayAllowsReplay in the keeper
// package for the settlement that this rules out.
func TestParamsValidateReplayInvariant(t *testing.T) {
	params := func(withdrawalDelay, retentionWindow time.Duration) Params {
		return NewParams(
			DefaultGasPerBlobByte,
			withdrawalDelay,
			DefaultPaymentPromiseTimeout,
			retentionWindow,
			DefaultPaymentPromiseHeightWindow,
			DefaultShardRetention,
		)
	}

	tests := []struct {
		name            string
		withdrawalDelay time.Duration
		retentionWindow time.Duration
		expectErr       bool
	}{
		{
			name:            "retention window comfortably above the floor",
			withdrawalDelay: 24 * time.Hour,
			retentionWindow: 48 * time.Hour,
			expectErr:       false,
		},
		{
			// The record is pruned no earlier than the promise stops being fresh,
			// so there is no window in which a replay can land.
			name:            "retention window exactly at the floor",
			withdrawalDelay: 24 * time.Hour,
			retentionWindow: 24*time.Hour + MaxPromiseClockSkew,
			expectErr:       false,
		},
		{
			name:            "retention window one nanosecond below the floor",
			withdrawalDelay: 24 * time.Hour,
			retentionWindow: 24*time.Hour + MaxPromiseClockSkew - time.Nanosecond,
			expectErr:       true,
		},
		{
			// Equal to the withdrawal delay is not enough: a promise settled with a
			// creation_timestamp up to MaxPromiseClockSkew ahead of block time has
			// its record pruned that much before it stops being fresh.
			name:            "retention window equal to withdrawal delay",
			withdrawalDelay: 24 * time.Hour,
			retentionWindow: 24 * time.Hour,
			expectErr:       true,
		},
		{
			name:            "retention window far shorter than withdrawal delay",
			withdrawalDelay: 24 * time.Hour,
			retentionWindow: time.Hour,
			expectErr:       true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := params(tt.withdrawalDelay, tt.retentionWindow).Validate()
			if tt.expectErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "payment promise retention window")
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestDefaultParamsAreValid(t *testing.T) {
	assert.NoError(t, DefaultParams().Validate())
}
