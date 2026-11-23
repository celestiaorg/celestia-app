package internal

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"time"
)

// FaultKind is the kind of fault a chaos step injects.
type FaultKind string

const (
	FaultPartition FaultKind = "partition" // cut the node off the network
	FaultDelay     FaultKind = "delay"     // add latency to every packet the node sends
	FaultLoss      FaultKind = "loss"      // drop a share of the packets the node sends
	FaultPause     FaultKind = "pause"     // freeze every process of the node
)

// Bounds for the delay and loss faults.
const (
	MinDelay       = 50 * time.Millisecond
	MaxDelay       = 500 * time.Millisecond
	MinLossPercent = 5
	MaxLossPercent = 40
)

// faultKinds is in a fixed order so a seed always picks the same kinds.
var faultKinds = []FaultKind{FaultPartition, FaultDelay, FaultLoss, FaultPause}

// FaultStep is one fault, applied to Targets for Duration and then healed.
type FaultStep struct {
	Index int
	Kind  FaultKind
	// Targets are validator indices, sorted.
	Targets  []int
	Duration time.Duration
	// Delay and Jitter are set for FaultDelay.
	Delay  time.Duration
	Jitter time.Duration
	// LossPercent is set for FaultLoss.
	LossPercent int
	// Halt marks a step that faults more than 1/3 of the voting power on
	// purpose: the chain is expected to halt and to recover after the heal.
	Halt bool
}

func (s FaultStep) String() string {
	b := fmt.Sprintf("step %d: %s validators %v for %s", s.Index, s.Kind, s.Targets, s.Duration)
	switch s.Kind {
	case FaultDelay:
		b += fmt.Sprintf(" (delay %s, jitter %s)", s.Delay, s.Jitter)
	case FaultLoss:
		b += fmt.Sprintf(" (loss %d%%)", s.LossPercent)
	}
	if s.Halt {
		b += " [halt then heal]"
	}
	return b
}

// FormatSchedule returns the schedule with one step per line.
func FormatSchedule(steps []FaultStep) string {
	lines := make([]string, len(steps))
	for i, s := range steps {
		lines[i] = s.String()
	}
	return strings.Join(lines, "\n")
}

// ScheduleConfig configures GenerateSchedule. All validators have equal voting power.
type ScheduleConfig struct {
	Validators int
	Steps      int
	// MinDuration and MaxDuration bound how long each fault lasts.
	MinDuration time.Duration
	MaxDuration time.Duration
	// HaltSteps is how many steps fault more than 1/3 of the voting power.
	HaltSteps int
}

// MaxFaulted returns the most validators of n equal validators that can be
// faulted while the others keep more than 2/3 of the voting power.
func MaxFaulted(n int) int { return (n - 1) / 3 }

// GenerateSchedule returns the fault steps for seed. The same seed and config
// always give the same steps, so a failed run can be replayed. It uses only
// the PCG output, not the helpers of math/rand, whose algorithms may change.
func GenerateSchedule(seed int64, cfg ScheduleConfig) ([]FaultStep, error) {
	switch {
	case MaxFaulted(cfg.Validators) < 1:
		return nil, fmt.Errorf("need at least 4 validators, got %d", cfg.Validators)
	case cfg.Steps < 1:
		return nil, errors.New("need at least one step")
	case cfg.MinDuration < time.Millisecond || cfg.MinDuration > cfg.MaxDuration:
		return nil, fmt.Errorf("invalid duration range [%s, %s]", cfg.MinDuration, cfg.MaxDuration)
	case cfg.HaltSteps < 0 || cfg.HaltSteps > cfg.Steps:
		return nil, fmt.Errorf("halt steps %d not in [0, %d]", cfg.HaltSteps, cfg.Steps)
	}
	pcg := rand.NewPCG(uint64(seed), 0)
	intn := func(n int) int { return int(pcg.Uint64() % uint64(n)) }
	between := func(lo, hi int64) int64 { return lo + int64(pcg.Uint64()%uint64(hi-lo+1)) }

	// Pick which steps halt the chain.
	halt := make([]bool, cfg.Steps)
	for placed := 0; placed < cfg.HaltSteps; {
		if i := intn(cfg.Steps); !halt[i] {
			halt[i] = true
			placed++
		}
	}

	steps := make([]FaultStep, cfg.Steps)
	for i := range steps {
		s := FaultStep{
			Index:    i,
			Duration: time.Duration(between(cfg.MinDuration.Milliseconds(), cfg.MaxDuration.Milliseconds())) * time.Millisecond,
			Halt:     halt[i],
		}
		count := 1 + intn(MaxFaulted(cfg.Validators))
		if s.Halt {
			// Only partition and pause stop a validator from voting.
			s.Kind = []FaultKind{FaultPartition, FaultPause}[intn(2)]
			count = MaxFaulted(cfg.Validators) + 1
		} else {
			s.Kind = faultKinds[intn(len(faultKinds))]
		}
		// Partial Fisher-Yates shuffle to pick count distinct validators.
		perm := make([]int, cfg.Validators)
		for j := range perm {
			perm[j] = j
		}
		for j := range count {
			k := j + intn(cfg.Validators-j)
			perm[j], perm[k] = perm[k], perm[j]
		}
		s.Targets = slices.Sorted(slices.Values(perm[:count]))

		switch s.Kind {
		case FaultDelay:
			s.Delay = time.Duration(between(int64(MinDelay/time.Millisecond), int64(MaxDelay/time.Millisecond))) * time.Millisecond
			s.Jitter = time.Duration(between(0, int64(s.Delay/4/time.Millisecond))) * time.Millisecond
		case FaultLoss:
			s.LossPercent = int(between(MinLossPercent, MaxLossPercent))
		}
		steps[i] = s
	}
	return steps, nil
}
