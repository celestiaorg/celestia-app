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
	badBlock  bool // diverge on the block hash instead of the app hash
	maxBlock  int64
}

func (n *fakeNode) Name() string { return n.name }

func (n *fakeNode) Height(context.Context) (int64, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.grow {
		n.height++
	}
	return n.height, nil
}

func (n *fakeNode) Block(_ context.Context, height int64) (BlockInfo, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if height > n.height {
		return BlockInfo{}, fmt.Errorf("%s: height %d is above the latest height %d", n.name, height, n.height)
	}
	if height > n.maxBlock {
		n.maxBlock = height
	}
	info := BlockInfo{BlockHash: fmt.Sprintf("block-%d", height), AppHash: fmt.Sprintf("app-%d", height)}
	if n.divergeAt > 0 && height >= n.divergeAt {
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
	return MonitorConfig{Interval: time.Millisecond, StallTimeout: 50 * time.Millisecond}
}

func TestMonitor(t *testing.T) {
	t.Run("healthy network passes", func(t *testing.T) {
		a := &fakeNode{name: "a", height: 1, grow: true}
		b := &fakeNode{name: "b", height: 1, grow: true}
		m := StartMonitor(context.Background(), nodes(a, b), testMonitorConfig())
		time.Sleep(30 * time.Millisecond)
		if err := m.Stop(); err != nil {
			t.Fatal(err)
		}
		if m.CheckedHeight() < 5 {
			t.Fatalf("expected the monitor to check some heights, got %d", m.CheckedHeight())
		}
	})
	t.Run("divergence names the first height and the node", func(t *testing.T) {
		m := StartMonitor(context.Background(), nodes(
			&fakeNode{name: "a", height: 1, grow: true},
			&fakeNode{name: "b", height: 1, grow: true, divergeAt: 7},
			&fakeNode{name: "c", height: 1, grow: true},
		), testMonitorConfig())
		time.Sleep(30 * time.Millisecond)
		requireContains(t, m.Stop(), "height 7", "b has forked-app-7")
	})
	t.Run("stall is detected", func(t *testing.T) {
		m := StartMonitor(context.Background(), nodes(
			&fakeNode{name: "a", height: 9},
			&fakeNode{name: "b", height: 8},
		), testMonitorConfig())
		time.Sleep(100 * time.Millisecond)
		requireContains(t, m.Stop(), "did not advance past height 9", "a=9", "b=8")
	})
	t.Run("nodes at different heights compare at the common height", func(t *testing.T) {
		lagging := &fakeNode{name: "lagging", height: 6}
		ahead := &fakeNode{name: "ahead", height: 6, grow: true}
		m := StartMonitor(context.Background(), nodes(ahead, lagging), MonitorConfig{Interval: time.Millisecond, StallTimeout: time.Minute})
		time.Sleep(30 * time.Millisecond)
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
		m := StartMonitor(context.Background(), nodes(
			&fakeNode{name: "a", height: 1, grow: true},
			&fakeNode{name: "b", height: 1, grow: true, divergeAt: 3},
		), MonitorConfig{Interval: time.Millisecond, StallTimeout: time.Minute, Every: 2})
		time.Sleep(30 * time.Millisecond)
		requireContains(t, m.Stop(), "height 4")
	})
}
