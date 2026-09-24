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

// TestPaymentPromiseRetentionWindowDerivation checks the retention window is
// derived as withdrawal_delay + MaxPromiseClockSkew for every valid delay.
func TestPaymentPromiseRetentionWindowDerivation(t *testing.T) {
	for _, withdrawalDelay := range []time.Duration{
		MinWithdrawalDelay,
		DefaultWithdrawalDelay,
		MaxWithdrawalDelay,
	} {
		params := DefaultParams()
		params.WithdrawalDelay = withdrawalDelay
		require.NoError(t, params.Validate())
		assert.Equal(t, withdrawalDelay+MaxPromiseClockSkew, params.PaymentPromiseRetentionWindow())
	}
}

// TestParamsValidateRejectsOverflowingWithdrawalDelay guards the derived
// retention window: a near-maximum delay would overflow
// withdrawal_delay + MaxPromiseClockSkew, but the upper bound rejects it first.
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
