package types

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_validateWithdrawalDelay(t *testing.T) {
	type test struct {
		name      string
		input     any
		expectErr bool
	}
	tests := []test{
		{
			name:      "default",
			input:     &DefaultWithdrawalDelay,
			expectErr: false,
		},
		{
			name:      "wrong type",
			input:     DefaultWithdrawalDelay,
			expectErr: true,
		},
		{
			name:      "nil",
			input:     (*time.Duration)(nil),
			expectErr: true,
		},
		{
			name:      "zero",
			input:     new(time.Duration(0)),
			expectErr: true,
		},
		{
			name:      "negative",
			input:     new(-time.Hour),
			expectErr: true,
		},
		{
			name:      "below the lower bound",
			input:     new(MinWithdrawalDelay - time.Nanosecond),
			expectErr: true,
		},
		{
			name:      "exactly at the lower bound",
			input:     new(MinWithdrawalDelay),
			expectErr: false,
		},
		{
			name:      "above the upper bound",
			input:     new(MaxWithdrawalDelay + time.Nanosecond),
			expectErr: true,
		},
		{
			name:      "exactly at the upper bound",
			input:     new(MaxWithdrawalDelay),
			expectErr: false,
		},
		{
			// Guards the replay invariant in Validate: an unbounded delay this
			// close to the maximum duration makes withdrawal_delay +
			// MaxPromiseClockSkew overflow negative, silently accepting retention
			// windows shorter than the freshness window.
			name:      "maximum duration",
			input:     new(time.Duration(math.MaxInt64)),
			expectErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateWithdrawalDelay(tt.input)
			if tt.expectErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func Test_validatePaymentPromiseTimeout(t *testing.T) {
	type test struct {
		name      string
		input     any
		expectErr bool
	}
	tests := []test{
		{
			name:      "default",
			input:     &DefaultPaymentPromiseTimeout,
			expectErr: false,
		},
		{
			name:      "wrong type",
			input:     DefaultPaymentPromiseTimeout,
			expectErr: true,
		},
		{
			name:      "nil",
			input:     (*time.Duration)(nil),
			expectErr: true,
		},
		{
			name:      "zero",
			input:     new(time.Duration(0)),
			expectErr: true,
		},
		{
			name:      "negative",
			input:     new(-time.Hour),
			expectErr: true,
		},
		{
			name:      "below the lower bound",
			input:     new(MinPaymentPromiseTimeout - time.Nanosecond),
			expectErr: true,
		},
		{
			name:      "exactly at the lower bound",
			input:     new(MinPaymentPromiseTimeout),
			expectErr: false,
		},
		{
			// Beyond this a promise outlives the withdrawal delay window and the
			// processed payment retention window that prevents double settlement.
			name:      "above the upper bound",
			input:     new(MaxPaymentPromiseTimeout + time.Nanosecond),
			expectErr: true,
		},
		{
			name:      "exactly at the upper bound",
			input:     new(MaxPaymentPromiseTimeout),
			expectErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validatePaymentPromiseTimeout(tt.input)
			if tt.expectErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func Test_validateShardRetention(t *testing.T) {
	type test struct {
		name      string
		input     any
		expectErr bool
	}
	tests := []test{
		{
			name:      "default",
			input:     &DefaultShardRetention,
			expectErr: false,
		},
		{
			name:      "wrong type",
			input:     DefaultShardRetention,
			expectErr: true,
		},
		{
			name:      "nil",
			input:     (*time.Duration)(nil),
			expectErr: true,
		},
		{
			name:      "zero",
			input:     new(time.Duration(0)),
			expectErr: true,
		},
		{
			name:      "negative",
			input:     new(-time.Hour),
			expectErr: true,
		},
		{
			name:      "below the lower bound",
			input:     new(MinShardRetention - time.Nanosecond),
			expectErr: true,
		},
		{
			name:      "exactly at the lower bound",
			input:     new(MinShardRetention),
			expectErr: false,
		},
		{
			name:      "above the upper bound",
			input:     new(MaxShardRetention + time.Nanosecond),
			expectErr: true,
		},
		{
			name:      "exactly at the upper bound",
			input:     new(MaxShardRetention),
			expectErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateShardRetention(tt.input)
			if tt.expectErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestParamsValidateReplayInvariant covers the relationship Params.Validate
// enforces between the payment promise retention window and the withdrawal
// delay. A retention window shorter than the withdrawal delay lets a settled
// promise be replayed once its processed payment record is pruned; see
// TestFutureDatedPromiseCannotBeReplayedAfterPruning in the keeper package for
// the settlement that this rules out.
func TestParamsValidateReplayInvariant(t *testing.T) {
	params := func(withdrawalDelay, retentionWindow time.Duration) Params {
		return NewParams(
			DefaultGasPerBlobByte,
			withdrawalDelay,
			DefaultPaymentPromiseTimeout,
			retentionWindow,
			DefaultPaymentPromiseHeightWindow,
			DefaultShardRetention,
			DefaultFullStakeStorageBudget,
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

// TestParamsValidateRejectsOverflowingWithdrawalDelay is a regression test for
// the replay invariant being bypassed by overflow. Before withdrawal_delay was
// bounded, a delay within MaxPromiseClockSkew of the maximum duration made
// withdrawal_delay + MaxPromiseClockSkew wrap to a negative floor, so Validate
// accepted a retention window far shorter than the freshness window and a
// settled promise could be replayed once its record was pruned.
func TestParamsValidateRejectsOverflowingWithdrawalDelay(t *testing.T) {
	params := DefaultParams()
	params.WithdrawalDelay = time.Duration(math.MaxInt64)
	require.Error(t, params.Validate())
}

// TestTimeoutSettlementWindowIsNeverEmpty pins the relationship between the
// withdrawal delay and the payment promise timeout bounds. MsgPaymentPromiseTimeout
// is only accepted between creation_timestamp + payment_promise_timeout and
// creation_timestamp + withdrawal_delay, so if the worst-case timeout could reach
// the smallest allowed delay that window would close and an expired promise could
// never be claimed. The per-param bounds alone have to rule that out.
func TestTimeoutSettlementWindowIsNeverEmpty(t *testing.T) {
	worstCase := MinWithdrawalDelay - MaxPaymentPromiseTimeout
	assert.GreaterOrEqual(t, worstCase, MinTimeoutSettlementWindow,
		"the narrowest valid withdrawal delay must exceed the widest valid promise timeout by at least MinTimeoutSettlementWindow")
}

func TestDefaultParamsAreValid(t *testing.T) {
	assert.NoError(t, DefaultParams().Validate())
}
