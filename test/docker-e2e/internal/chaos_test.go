package internal

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func testScheduleConfig(validators int) ScheduleConfig {
	return ScheduleConfig{
		Validators:  validators,
		Steps:       20,
		MinDuration: 5 * time.Second,
		MaxDuration: 30 * time.Second,
		HaltSteps:   1,
	}
}

func TestGenerateScheduleIsDeterministic(t *testing.T) {
	cfg := testScheduleConfig(4)
	a, err := GenerateSchedule(42, cfg)
	if err != nil {
		t.Fatal(err)
	}
	b, err := GenerateSchedule(42, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("same seed gave different schedules:\n%s\n%s", FormatSchedule(a), FormatSchedule(b))
	}
	c, err := GenerateSchedule(43, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if reflect.DeepEqual(a, c) {
		t.Fatalf("seeds 42 and 43 gave the same schedule:\n%s", FormatSchedule(a))
	}
}

// TestGenerateScheduleGolden pins the schedule for one seed, so a change to
// the generator that would stop an old seed from replaying is visible.
func TestGenerateScheduleGolden(t *testing.T) {
	cfg := testScheduleConfig(4)
	cfg.Steps = 3
	steps, err := GenerateSchedule(7, cfg)
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Join([]string{
		"step 0: delay validators [2] for 9.234s (delay 206ms, jitter 51ms)",
		"step 1: partition validators [0 1] for 13.588s [halt then heal]",
		"step 2: pause validators [2] for 5.025s",
	}, "\n")
	if got := FormatSchedule(steps); got != want {
		t.Fatalf("schedule for seed 7 changed:\n got:\n%s\nwant:\n%s", got, want)
	}
}

func TestGenerateScheduleBounds(t *testing.T) {
	kinds := map[FaultKind]bool{}
	for _, validators := range []int{4, 5, 7, 10} {
		cfg := testScheduleConfig(validators)
		for seed := int64(0); seed < 500; seed++ {
			steps, err := GenerateSchedule(seed, cfg)
			if err != nil {
				t.Fatalf("seed %d: %v", seed, err)
			}
			if len(steps) != cfg.Steps {
				t.Fatalf("seed %d: got %d steps, want %d", seed, len(steps), cfg.Steps)
			}
			halts := 0
			for i, s := range steps {
				if s.Index != i {
					t.Fatalf("seed %d: step %d has index %d", seed, i, s.Index)
				}
				if s.Duration < cfg.MinDuration || s.Duration > cfg.MaxDuration {
					t.Fatalf("seed %d step %d: duration %s outside [%s, %s]", seed, i, s.Duration, cfg.MinDuration, cfg.MaxDuration)
				}
				if len(s.Targets) == 0 {
					t.Fatalf("seed %d step %d: no targets", seed, i)
				}
				seen := map[int]bool{}
				for _, v := range s.Targets {
					if v < 0 || v >= validators || seen[v] {
						t.Fatalf("seed %d step %d: bad targets %v", seed, i, s.Targets)
					}
					seen[v] = true
				}
				// Equal voting power: faulted power stays below 1/3 unless the
				// step is a halt-then-heal step, which must exceed 1/3.
				if s.Halt {
					halts++
					if 3*len(s.Targets) <= validators {
						t.Fatalf("seed %d step %d: halt step faults only %d of %d", seed, i, len(s.Targets), validators)
					}
					if s.Kind != FaultPartition && s.Kind != FaultPause {
						t.Fatalf("seed %d step %d: halt step has kind %s", seed, i, s.Kind)
					}
				} else if 3*len(s.Targets) >= validators {
					t.Fatalf("seed %d step %d: faults %d of %d validators", seed, i, len(s.Targets), validators)
				}
				kinds[s.Kind] = true
				switch s.Kind {
				case FaultDelay:
					if s.Delay < MinDelay || s.Delay > MaxDelay || s.Jitter < 0 || s.Jitter > s.Delay/4 {
						t.Fatalf("seed %d step %d: delay %s jitter %s out of bounds", seed, i, s.Delay, s.Jitter)
					}
				case FaultLoss:
					if s.LossPercent < MinLossPercent || s.LossPercent > MaxLossPercent {
						t.Fatalf("seed %d step %d: loss %d%% out of bounds", seed, i, s.LossPercent)
					}
				case FaultPartition, FaultPause:
				default:
					t.Fatalf("seed %d step %d: unknown kind %q", seed, i, s.Kind)
				}
			}
			if halts != cfg.HaltSteps {
				t.Fatalf("seed %d: got %d halt steps, want %d", seed, halts, cfg.HaltSteps)
			}
		}
	}
	if len(kinds) != len(faultKinds) {
		t.Fatalf("got kinds %v, want all of %v", kinds, faultKinds)
	}
}

func TestGenerateScheduleRejectsBadConfig(t *testing.T) {
	for name, mutate := range map[string]func(*ScheduleConfig){
		"too few validators": func(c *ScheduleConfig) { c.Validators = 3 },
		"no steps":           func(c *ScheduleConfig) { c.Steps = 0 },
		"zero duration":      func(c *ScheduleConfig) { c.MinDuration = 0 },
		"min above max":      func(c *ScheduleConfig) { c.MinDuration = time.Minute },
		"too many halts":     func(c *ScheduleConfig) { c.HaltSteps = c.Steps + 1 },
		"negative halts":     func(c *ScheduleConfig) { c.HaltSteps = -1 },
	} {
		t.Run(name, func(t *testing.T) {
			cfg := testScheduleConfig(4)
			mutate(&cfg)
			if _, err := GenerateSchedule(1, cfg); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}
