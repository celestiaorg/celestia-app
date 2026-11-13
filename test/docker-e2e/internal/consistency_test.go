package internal

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeNode is a node whose blocks are derived from the height. From
// divergeAt on, it reports a different app hash or block hash.
type fakeNode struct {
	name      string
	mu        sync.Mutex
	height    int64
	grow      bool // advance one height per Height call
	divergeAt int64
	onlyAt    bool // diverge only at divergeAt, not above it
	badBlock  bool // diverge on the block hash instead of the app hash
	down      bool // fail every RPC
	blockErrs int  // fail this many Block calls; negative fails all of them
	maxBlock  int64
}

func (n *fakeNode) set(f func(n *fakeNode)) {
	n.mu.Lock()
	defer n.mu.Unlock()
	f(n)
}

func (n *fakeNode) currentHeight() int64 {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.height
}

func (n *fakeNode) Name() string { return n.name }

func (n *fakeNode) Height(context.Context) (int64, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.down {
		return 0, fmt.Errorf("%s is down", n.name)
	}
	if n.grow {
		n.height++
	}
	return n.height, nil
}

func (n *fakeNode) Block(_ context.Context, height int64) (BlockInfo, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.down {
		return BlockInfo{}, fmt.Errorf("%s is down", n.name)
	}
	if n.blockErrs != 0 {
		n.blockErrs--
		return BlockInfo{}, fmt.Errorf("%s block query failed", n.name)
	}
	if height > n.height {
		return BlockInfo{}, fmt.Errorf("%s: height %d is above the latest height %d", n.name, height, n.height)
	}
	if height > n.maxBlock {
		n.maxBlock = height
	}
	info := BlockInfo{BlockHash: fmt.Sprintf("block-%d", height), AppHash: fmt.Sprintf("app-%d", height)}
	if n.divergeAt > 0 && (height == n.divergeAt || (!n.onlyAt && height > n.divergeAt)) {
		if n.badBlock {
			info.BlockHash = "forked-" + info.BlockHash
		} else {
			info.AppHash = "forked-" + info.AppHash
		}
	}
	return info, nil
}

func nodes(ns ...*fakeNode) []Node {
	out := make([]Node, len(ns))
	for i, n := range ns {
		out[i] = n
	}
	return out
}

func requireContains(t *testing.T, err error, parts ...string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected an error containing %q, got nil", parts)
	}
	for _, p := range parts {
		if !strings.Contains(err.Error(), p) {
			t.Fatalf("error %q does not contain %q", err, p)
		}
	}
}

func TestCheckAgreement(t *testing.T) {
	ctx := context.Background()
	t.Run("healthy", func(t *testing.T) {
		err := CheckAgreement(ctx, nodes(&fakeNode{name: "a", height: 10}, &fakeNode{name: "b", height: 10}), 10)
		if err != nil {
			t.Fatal(err)
		}
	})
	t.Run("app hash divergence names the node", func(t *testing.T) {
		err := CheckAgreement(ctx, nodes(
			&fakeNode{name: "a", height: 10},
			&fakeNode{name: "b", height: 10, divergeAt: 5},
			&fakeNode{name: "c", height: 10},
		), 5)
		requireContains(t, err, "height 5", "app hash", "b has forked-app-5", "a, c have app-5")
		div, ok := errors.AsType[*DivergenceError](err)
		if !ok || div.Height != 5 || div.Node != "b" {
			t.Fatalf("expected a DivergenceError for b at height 5, got %v", err)
		}
	})
	t.Run("block hash divergence", func(t *testing.T) {
		err := CheckAgreement(ctx, nodes(
			&fakeNode{name: "a", height: 10, divergeAt: 3, badBlock: true},
			&fakeNode{name: "b", height: 10},
			&fakeNode{name: "c", height: 10},
		), 3)
		requireContains(t, err, "height 3", "block hash", "a has forked-block-3")
	})
}

func TestCheckRangeReportsFirstDivergingHeight(t *testing.T) {
	err := CheckRange(context.Background(), nodes(
		&fakeNode{name: "a", height: 20},
		&fakeNode{name: "b", height: 20},
		&fakeNode{name: "c", height: 20, divergeAt: 12},
	), 1, 20)
	requireContains(t, err, "height 12", "c has forked-app-12")
}

func testMonitorConfig() MonitorConfig {
	return MonitorConfig{Interval: time.Millisecond, StallTimeout: time.Minute}
}

// eventually waits up to five seconds for cond, so slow runners do not fail.
func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

func failed(m *Monitor) func() bool { return func() bool { return m.Err() != nil } }

func TestMonitor(t *testing.T) {
	t.Run("healthy network passes", func(t *testing.T) {
		m := StartMonitor(context.Background(), nodes(
			&fakeNode{name: "a", height: 1, grow: true},
			&fakeNode{name: "b", height: 1, grow: true},
		), testMonitorConfig())
		eventually(t, "height 5 to be checked", func() bool { return m.CheckedHeight() >= 5 })
		if err := m.Stop(); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("divergence names the first height and the node", func(t *testing.T) {
		m := StartMonitor(context.Background(), nodes(
			&fakeNode{name: "a", height: 1, grow: true},
			&fakeNode{name: "b", height: 1, grow: true, divergeAt: 7},
			&fakeNode{name: "c", height: 1, grow: true},
		), testMonitorConfig())
		eventually(t, "the monitor to fail", failed(m))
		requireContains(t, m.Stop(), "height 7", "b has forked-app-7")
	})
	t.Run("stall is detected", func(t *testing.T) {
		cfg := testMonitorConfig()
		cfg.StallTimeout = 20 * time.Millisecond
		m := StartMonitor(context.Background(), nodes(
			&fakeNode{name: "a", height: 9},
			&fakeNode{name: "b", height: 8},
		), cfg)
		eventually(t, "the monitor to fail", failed(m))
		requireContains(t, m.Stop(), "did not advance past height 9", "a=9", "b=8")
	})
	t.Run("nodes at different heights compare at the common height", func(t *testing.T) {
		lagging := &fakeNode{name: "lagging", height: 6}
		ahead := &fakeNode{name: "ahead", height: 6, grow: true}
		m := StartMonitor(context.Background(), nodes(ahead, lagging), testMonitorConfig())
		eventually(t, "the leading node to pass height 20", func() bool { return ahead.currentHeight() > 20 })
		if err := m.Stop(); err != nil {
			t.Fatal(err)
		}
		if got := m.CheckedHeight(); got != 6 {
			t.Fatalf("expected heights up to the common height 6 to be checked, got %d", got)
		}
		if lagging.maxBlock != 6 {
			t.Fatalf("expected the lagging node to be asked for heights up to 6, got %d", lagging.maxBlock)
		}
	})
	t.Run("checks every N heights", func(t *testing.T) {
		// The divergence at height 3 is skipped; the one from height 3 on is
		// still caught at the first checked height, 4.
		cfg := testMonitorConfig()
		cfg.Every = 2
		m := StartMonitor(context.Background(), nodes(
			&fakeNode{name: "a", height: 1, grow: true},
			&fakeNode{name: "b", height: 1, grow: true, divergeAt: 3},
		), cfg)
		eventually(t, "the monitor to fail", failed(m))
		requireContains(t, m.Stop(), "height 4")
	})
	t.Run("heights a node missed while unreachable are checked when it returns", func(t *testing.T) {
		a := &fakeNode{name: "a", height: 1, grow: true}
		b := &fakeNode{name: "b", height: 1, down: true, divergeAt: 5, onlyAt: true}
		m := StartMonitor(context.Background(), nodes(a, b), testMonitorConfig())
		eventually(t, "a to pass height 20", func() bool { return a.currentHeight() > 20 })
		b.set(func(n *fakeNode) { n.down, n.grow = false, true })
		eventually(t, "the monitor to fail", failed(m))
		requireContains(t, m.Stop(), "height 5", "b has forked-app-5")
	})
	t.Run("a node unreachable longer than the stall timeout fails", func(t *testing.T) {
		cfg := testMonitorConfig()
		cfg.StallTimeout = 20 * time.Millisecond
		m := StartMonitor(context.Background(), nodes(
			&fakeNode{name: "a", height: 1, grow: true},
			&fakeNode{name: "b", height: 1, down: true},
		), cfg)
		eventually(t, "the monitor to fail", failed(m))
		requireContains(t, m.Stop(), "b did not answer for", "a=")
	})
	t.Run("block queries that keep failing fail the monitor", func(t *testing.T) {
		cfg := testMonitorConfig()
		cfg.StallTimeout = 20 * time.Millisecond
		m := StartMonitor(context.Background(), nodes(
			&fakeNode{name: "a", height: 1, grow: true},
			&fakeNode{name: "b", height: 1, grow: true, blockErrs: -1},
		), cfg)
		eventually(t, "the monitor to fail", failed(m))
		requireContains(t, m.Stop(), "agreement checks are stuck at height 1", "b block query failed", "a=", "b=")
	})
	t.Run("a transient block query error is retried", func(t *testing.T) {
		cfg := testMonitorConfig()
		cfg.StallTimeout = time.Second
		a := &fakeNode{name: "a", height: 1, grow: true}
		b := &fakeNode{name: "b", height: 1, grow: true, blockErrs: 3}
		m := StartMonitor(context.Background(), nodes(a, b), cfg)
		eventually(t, "height 10 to be checked", func() bool { return m.CheckedHeight() >= 10 })
		if err := m.Stop(); err != nil {
			t.Fatal(err)
		}
		if b.maxBlock < 10 {
			t.Fatalf("expected b to be asked for heights up to 10, got %d", b.maxBlock)
		}
	})
}

func TestMonitorStop(t *testing.T) {
	// Interval is long, so only the first poll runs before Stop.
	cfg := MonitorConfig{Interval: time.Hour, StallTimeout: time.Hour, FinalTimeout: time.Second}
	t.Run("checks the blocks committed since the last poll", func(t *testing.T) {
		a := &fakeNode{name: "a", height: 3}
		b := &fakeNode{name: "b", height: 3, divergeAt: 8}
		m := StartMonitor(context.Background(), nodes(a, b), cfg)
		eventually(t, "height 3 to be checked", func() bool { return m.CheckedHeight() == 3 })
		a.set(func(n *fakeNode) { n.height = 10 })
		b.set(func(n *fakeNode) { n.height = 10 })
		requireContains(t, m.Stop(), "height 8", "b has forked-app-8")
	})
	t.Run("checks up to the final common height", func(t *testing.T) {
		a := &fakeNode{name: "a", height: 3}
		b := &fakeNode{name: "b", height: 3}
		m := StartMonitor(context.Background(), nodes(a, b), cfg)
		eventually(t, "height 3 to be checked", func() bool { return m.CheckedHeight() == 3 })
		a.set(func(n *fakeNode) { n.height = 12 })
		b.set(func(n *fakeNode) { n.height = 10 })
		if err := m.Stop(); err != nil {
			t.Fatal(err)
		}
		if got := m.CheckedHeight(); got != 10 {
			t.Fatalf("expected the final check to reach height 10, got %d", got)
		}
	})
	t.Run("an RPC error at teardown is not a failure", func(t *testing.T) {
		a := &fakeNode{name: "a", height: 3}
		b := &fakeNode{name: "b", height: 3}
		m := StartMonitor(context.Background(), nodes(a, b), cfg)
		eventually(t, "height 3 to be checked", func() bool { return m.CheckedHeight() == 3 })
		a.set(func(n *fakeNode) { n.height = 10 })
		b.set(func(n *fakeNode) { n.height, n.down = 10, true })
		if err := m.Stop(); err != nil {
			t.Fatal(err)
		}
	})
}
