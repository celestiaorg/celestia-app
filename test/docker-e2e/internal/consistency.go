package internal

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// BlockInfo is what one node reports for a committed block.
type BlockInfo struct {
	BlockHash string
	AppHash   string
}

// Node is one node of a chain, as seen by the consistency checks.
type Node interface {
	Name() string
	// Height returns the latest committed height.
	Height(ctx context.Context) (int64, error)
	// Block returns the block at height.
	Block(ctx context.Context, height int64) (BlockInfo, error)
}

// DivergenceError reports that a node disagrees with the other nodes at a height.
type DivergenceError struct {
	Height int64
	Node   string
	Field  string // "app hash" or "block hash"
	Got    string
	Want   string
	Agreed []string // nodes that have Want
}

func (e *DivergenceError) Error() string {
	return fmt.Sprintf("%s mismatch at height %d: %s has %s, %s have %s",
		e.Field, e.Height, e.Node, e.Got, strings.Join(e.Agreed, ", "), e.Want)
}

// CheckAgreement checks that all nodes have the same app hash and block hash
// at height. Every node must have committed height.
func CheckAgreement(ctx context.Context, nodes []Node, height int64) error {
	infos := make([]BlockInfo, len(nodes))
	for i, n := range nodes {
		info, err := n.Block(ctx, height)
		if err != nil {
			return fmt.Errorf("failed to get block %d from %s: %w", height, n.Name(), err)
		}
		infos[i] = info
	}
	if err := compare(nodes, infos, height, "app hash", func(b BlockInfo) string { return b.AppHash }); err != nil {
		return err
	}
	return compare(nodes, infos, height, "block hash", func(b BlockInfo) string { return b.BlockHash })
}

// CheckRange runs CheckAgreement for each height from from to to and returns
// the error for the first height that diverges.
func CheckRange(ctx context.Context, nodes []Node, from, to int64) error {
	for h := from; h <= to; h++ {
		if err := CheckAgreement(ctx, nodes, h); err != nil {
			return err
		}
	}
	return nil
}

// compare finds the value most nodes agree on (the first node's value wins a
// tie) and returns a DivergenceError for the first node that differs from it.
func compare(nodes []Node, infos []BlockInfo, height int64, field string, value func(BlockInfo) string) error {
	counts := make(map[string]int)
	want := ""
	for _, info := range infos {
		v := value(info)
		counts[v]++
		if want == "" || counts[v] > counts[want] {
			want = v
		}
	}
	if len(counts) <= 1 {
		return nil
	}
	var agreed []string
	for i, n := range nodes {
		if value(infos[i]) == want {
			agreed = append(agreed, n.Name())
		}
	}
	for i, n := range nodes {
		if got := value(infos[i]); got != want {
			return &DivergenceError{Height: height, Node: n.Name(), Field: field, Got: got, Want: want, Agreed: agreed}
		}
	}
	return nil
}

// MonitorConfig configures a Monitor.
type MonitorConfig struct {
	// Interval is the time between two polls of the nodes.
	Interval time.Duration
	// StallTimeout is how long the highest node height may stay the same
	// before the monitor reports a stalled chain.
	StallTimeout time.Duration
	// Every checks agreement only at heights that are a multiple of Every.
	// Zero or one checks every height.
	Every int64
}

// Monitor checks in the background that the nodes agree on every committed
// height and that the chain keeps producing blocks.
type Monitor struct {
	nodes  []Node
	cfg    MonitorConfig
	cancel context.CancelFunc
	done   chan struct{}

	mu      sync.Mutex
	err     error
	checked int64
}

// StartMonitor starts a Monitor. Call Stop to end it and get its result.
func StartMonitor(ctx context.Context, nodes []Node, cfg MonitorConfig) *Monitor {
	if cfg.Every < 1 {
		cfg.Every = 1
	}
	ctx, cancel := context.WithCancel(ctx)
	m := &Monitor{nodes: nodes, cfg: cfg, cancel: cancel, done: make(chan struct{})}
	go m.run(ctx)
	return m
}

// Stop ends the monitor and returns the first divergence or stall it found.
func (m *Monitor) Stop() error {
	m.cancel()
	<-m.done
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.err
}

// CheckedHeight returns the highest height up to which agreement was checked.
func (m *Monitor) CheckedHeight() int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.checked
}

func (m *Monitor) run(ctx context.Context) {
	defer close(m.done)
	ticker := time.NewTicker(m.cfg.Interval)
	defer ticker.Stop()

	var lastHeight int64
	lastAdvance := time.Now()
	for {
		heights, reached := m.heights(ctx)
		if ctx.Err() != nil {
			return
		}

		// Liveness: the highest height must advance within StallTimeout.
		var highest int64
		for _, h := range heights {
			highest = max(highest, h)
		}
		if highest > lastHeight {
			lastHeight, lastAdvance = highest, time.Now()
		} else if time.Since(lastAdvance) > m.cfg.StallTimeout {
			m.fail(fmt.Errorf("chain did not advance past height %d in %s: %s",
				lastHeight, m.cfg.StallTimeout, m.formatHeights(heights)))
			return
		}

		// Agreement: compare the reachable nodes up to their common height.
		if len(reached) > 0 {
			common := heights[reached[0].Name()]
			for _, n := range reached {
				common = min(common, heights[n.Name()])
			}
			if err := m.checkUpTo(ctx, reached, common); err != nil {
				m.fail(err)
				return
			}
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// heights returns the height of each node that answered, and those nodes.
// A node that does not answer (for example while it restarts) is skipped.
func (m *Monitor) heights(ctx context.Context) (map[string]int64, []Node) {
	heights := make(map[string]int64, len(m.nodes))
	var reached []Node
	for _, n := range m.nodes {
		h, err := n.Height(ctx)
		if err != nil || h <= 0 {
			continue
		}
		heights[n.Name()] = h
		reached = append(reached, n)
	}
	return heights, reached
}

// checkUpTo checks the heights above the last checked one up to common. It
// returns only divergence errors; RPC errors are retried at the next poll.
func (m *Monitor) checkUpTo(ctx context.Context, nodes []Node, common int64) error {
	for h := m.CheckedHeight() + 1; h <= common; h++ {
		if h%m.cfg.Every == 0 {
			err := CheckAgreement(ctx, nodes, h)
			if _, ok := errors.AsType[*DivergenceError](err); ok {
				return err
			}
			if err != nil {
				return nil
			}
		}
		m.mu.Lock()
		m.checked = h
		m.mu.Unlock()
	}
	return nil
}

func (m *Monitor) fail(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.err = err
}

func (m *Monitor) formatHeights(heights map[string]int64) string {
	parts := make([]string, len(m.nodes))
	for i, n := range m.nodes {
		if h, ok := heights[n.Name()]; ok {
			parts[i] = fmt.Sprintf("%s=%d", n.Name(), h)
		} else {
			parts[i] = n.Name() + "=unreachable"
		}
	}
	return strings.Join(parts, ", ")
}
