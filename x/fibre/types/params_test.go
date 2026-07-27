package types

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

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

func TestDefaultParamsAreValid(t *testing.T) {
	assert.NoError(t, DefaultParams().Validate())
}
