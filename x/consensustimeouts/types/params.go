package types

import (
	"fmt"
	"time"

	"github.com/celestiaorg/celestia-app/v9/pkg/appconsts"
)

var (
	DefaultTimeoutPropose          = appconsts.DefaultTimeoutPropose
	DefaultTimeoutProposeDelta     = appconsts.DefaultTimeoutProposeDelta
	DefaultTimeoutPrevote          = appconsts.DefaultTimeoutPrevote
	DefaultTimeoutPrevoteDelta     = appconsts.DefaultTimeoutPrevoteDelta
	DefaultTimeoutPrecommit        = appconsts.DefaultTimeoutPrecommit
	DefaultTimeoutPrecommitDelta   = appconsts.DefaultTimeoutPrecommitDelta
	DefaultTimeoutCommit           = appconsts.DefaultTimeoutCommit
	DefaultDelayedPrecommitTimeout = appconsts.DefaultDelayedPrecommitTimeout
)

// Bounds enforced by Params.Validate. Chosen during the design interview.
const (
	minTimeoutPropose          = 500 * time.Millisecond
	maxTimeoutPropose          = 10 * time.Second
	minTimeoutProposeDelta     = 100 * time.Millisecond
	maxTimeoutProposeDelta     = 10 * time.Second
	minTimeoutPrevote          = 500 * time.Millisecond
	maxTimeoutPrevote          = 10 * time.Second
	minTimeoutPrevoteDelta     = 100 * time.Millisecond
	maxTimeoutPrevoteDelta     = 10 * time.Second
	minTimeoutPrecommit        = 500 * time.Millisecond
	maxTimeoutPrecommit        = 10 * time.Second
	minTimeoutPrecommitDelta   = 100 * time.Millisecond
	maxTimeoutPrecommitDelta   = 10 * time.Second
	minTimeoutCommit           = 1 * time.Millisecond
	maxTimeoutCommit           = 2 * time.Second
	minDelayedPrecommitTimeout = 100 * time.Millisecond
	maxDelayedPrecommitTimeout = 10 * time.Second
)

// NewParams constructs a Params with the supplied timeout durations.
func NewParams(
	propose, proposeDelta,
	prevote, prevoteDelta,
	precommit, precommitDelta,
	commit, delayedPrecommit time.Duration,
) Params {
	return Params{
		TimeoutPropose:          propose,
		TimeoutProposeDelta:     proposeDelta,
		TimeoutPrevote:          prevote,
		TimeoutPrevoteDelta:     prevoteDelta,
		TimeoutPrecommit:        precommit,
		TimeoutPrecommitDelta:   precommitDelta,
		TimeoutCommit:           commit,
		DelayedPrecommitTimeout: delayedPrecommit,
	}
}

// DefaultParams returns Params populated from the appconsts defaults.
func DefaultParams() Params {
	return NewParams(
		DefaultTimeoutPropose, DefaultTimeoutProposeDelta,
		DefaultTimeoutPrevote, DefaultTimeoutPrevoteDelta,
		DefaultTimeoutPrecommit, DefaultTimeoutPrecommitDelta,
		DefaultTimeoutCommit, DefaultDelayedPrecommitTimeout,
	)
}

// Validate checks each timeout field is within its allowed range.
func (p Params) Validate() error {
	fields := []struct {
		name     string
		value    time.Duration
		min, max time.Duration
	}{
		{"timeout_propose", p.TimeoutPropose, minTimeoutPropose, maxTimeoutPropose},
		{"timeout_propose_delta", p.TimeoutProposeDelta, minTimeoutProposeDelta, maxTimeoutProposeDelta},
		{"timeout_prevote", p.TimeoutPrevote, minTimeoutPrevote, maxTimeoutPrevote},
		{"timeout_prevote_delta", p.TimeoutPrevoteDelta, minTimeoutPrevoteDelta, maxTimeoutPrevoteDelta},
		{"timeout_precommit", p.TimeoutPrecommit, minTimeoutPrecommit, maxTimeoutPrecommit},
		{"timeout_precommit_delta", p.TimeoutPrecommitDelta, minTimeoutPrecommitDelta, maxTimeoutPrecommitDelta},
		{"timeout_commit", p.TimeoutCommit, minTimeoutCommit, maxTimeoutCommit},
		{"delayed_precommit_timeout", p.DelayedPrecommitTimeout, minDelayedPrecommitTimeout, maxDelayedPrecommitTimeout},
	}
	for _, f := range fields {
		if f.value < f.min || f.value > f.max {
			return fmt.Errorf("%s must be in [%s, %s], got %s", f.name, f.min, f.max, f.value)
		}
	}
	return nil
}
