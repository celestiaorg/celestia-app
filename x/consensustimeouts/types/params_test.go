package types_test

import (
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v9/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v9/x/consensustimeouts/types"
	"github.com/stretchr/testify/require"
)

// TestDefaultParams_MatchesAppConsts asserts each field of DefaultParams()
// equals the corresponding appconsts.DefaultTimeout* constant.
func TestDefaultParams_MatchesAppConsts(t *testing.T) {
	p := types.DefaultParams()

	require.Equal(t, appconsts.DefaultTimeoutPropose, p.TimeoutPropose, "TimeoutPropose")
	require.Equal(t, appconsts.DefaultTimeoutProposeDelta, p.TimeoutProposeDelta, "TimeoutProposeDelta")
	require.Equal(t, appconsts.DefaultTimeoutPrevote, p.TimeoutPrevote, "TimeoutPrevote")
	require.Equal(t, appconsts.DefaultTimeoutPrevoteDelta, p.TimeoutPrevoteDelta, "TimeoutPrevoteDelta")
	require.Equal(t, appconsts.DefaultTimeoutPrecommit, p.TimeoutPrecommit, "TimeoutPrecommit")
	require.Equal(t, appconsts.DefaultTimeoutPrecommitDelta, p.TimeoutPrecommitDelta, "TimeoutPrecommitDelta")
	require.Equal(t, appconsts.DefaultTimeoutCommit, p.TimeoutCommit, "TimeoutCommit")
	require.Equal(t, appconsts.DefaultDelayedPrecommitTimeout, p.DelayedPrecommitTimeout, "DelayedPrecommitTimeout")
}

// TestValidate_NewParams_HappyPath verifies NewParams with CIP-048 defaults
// constructs a Params value that passes Validate().
func TestValidate_NewParams_HappyPath(t *testing.T) {
	p := types.NewParams(
		appconsts.DefaultTimeoutPropose,
		appconsts.DefaultTimeoutProposeDelta,
		appconsts.DefaultTimeoutPrevote,
		appconsts.DefaultTimeoutPrevoteDelta,
		appconsts.DefaultTimeoutPrecommit,
		appconsts.DefaultTimeoutPrecommitDelta,
		appconsts.DefaultTimeoutCommit,
		appconsts.DefaultDelayedPrecommitTimeout,
	)
	require.NoError(t, p.Validate())
}

// TestValidate_BoundsTable exercises each field through the canonical seven
// scenarios (below-min, at-min, mid, at-max, above-max, zero, negative). For
// every row the test mutates a single field starting from DefaultParams() and
// asserts the expected pass/fail outcome and that any error message names the
// offending field.
func TestValidate_BoundsTable(t *testing.T) {
	type fieldSpec struct {
		name   string
		jsonID string // protobuf JSON name, surfaced in error messages
		set    func(p *types.Params, v time.Duration)
		min    time.Duration
		max    time.Duration
	}

	fields := []fieldSpec{
		{
			name:   "TimeoutPropose",
			jsonID: "timeout_propose",
			set:    func(p *types.Params, v time.Duration) { p.TimeoutPropose = v },
			min:    500 * time.Millisecond,
			max:    10 * time.Second,
		},
		{
			name:   "TimeoutProposeDelta",
			jsonID: "timeout_propose_delta",
			set:    func(p *types.Params, v time.Duration) { p.TimeoutProposeDelta = v },
			min:    100 * time.Millisecond,
			max:    10 * time.Second,
		},
		{
			name:   "TimeoutPrevote",
			jsonID: "timeout_prevote",
			set:    func(p *types.Params, v time.Duration) { p.TimeoutPrevote = v },
			min:    500 * time.Millisecond,
			max:    10 * time.Second,
		},
		{
			name:   "TimeoutPrevoteDelta",
			jsonID: "timeout_prevote_delta",
			set:    func(p *types.Params, v time.Duration) { p.TimeoutPrevoteDelta = v },
			min:    100 * time.Millisecond,
			max:    10 * time.Second,
		},
		{
			name:   "TimeoutPrecommit",
			jsonID: "timeout_precommit",
			set:    func(p *types.Params, v time.Duration) { p.TimeoutPrecommit = v },
			min:    500 * time.Millisecond,
			max:    10 * time.Second,
		},
		{
			name:   "TimeoutPrecommitDelta",
			jsonID: "timeout_precommit_delta",
			set:    func(p *types.Params, v time.Duration) { p.TimeoutPrecommitDelta = v },
			min:    100 * time.Millisecond,
			max:    10 * time.Second,
		},
		{
			name:   "TimeoutCommit",
			jsonID: "timeout_commit",
			set:    func(p *types.Params, v time.Duration) { p.TimeoutCommit = v },
			min:    1 * time.Millisecond,
			max:    2 * time.Second,
		},
		{
			name:   "DelayedPrecommitTimeout",
			jsonID: "delayed_precommit_timeout",
			set:    func(p *types.Params, v time.Duration) { p.DelayedPrecommitTimeout = v },
			min:    100 * time.Millisecond,
			max:    10 * time.Second,
		},
	}

	type scenario struct {
		name      string
		value     func(min, max time.Duration) time.Duration
		expectErr bool
	}

	scenarios := []scenario{
		{
			name:      "below-min",
			value:     func(min, _ time.Duration) time.Duration { return min - 1 },
			expectErr: true,
		},
		{
			name:      "at-min",
			value:     func(min, _ time.Duration) time.Duration { return min },
			expectErr: false,
		},
		{
			name:      "mid",
			value:     func(min, max time.Duration) time.Duration { return (min + max) / 2 },
			expectErr: false,
		},
		{
			name:      "at-max",
			value:     func(_, max time.Duration) time.Duration { return max },
			expectErr: false,
		},
		{
			name:      "above-max",
			value:     func(_, max time.Duration) time.Duration { return max + 1 },
			expectErr: true,
		},
		{
			name:      "zero",
			value:     func(_, _ time.Duration) time.Duration { return 0 },
			expectErr: true,
		},
		{
			name:      "negative",
			value:     func(_, _ time.Duration) time.Duration { return -1 * time.Second },
			expectErr: true,
		},
	}

	for _, f := range fields {
		for _, s := range scenarios {
			t.Run(f.name+"/"+s.name, func(t *testing.T) {
				p := types.DefaultParams()
				value := s.value(f.min, f.max)
				f.set(&p, value)

				err := p.Validate()
				if s.expectErr {
					require.Error(t, err, "expected error for %s=%s", f.name, value)
					require.Contains(t, err.Error(), f.jsonID, "error message should name field %s", f.jsonID)
				} else {
					require.NoError(t, err, "did not expect error for %s=%s", f.name, value)
				}
			})
		}
	}
}
