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
	// FinalTimeout bounds the last agreement check that Stop runs.
	// Zero means 30 seconds.
	FinalTimeout time.Duration
}

// Monitor checks in the background that the nodes agree on every committed
// height and that the chain keeps producing blocks.
type Monitor struct {
	nodes  []Node
	cfg    MonitorConfig
	parent context.Context
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
	if cfg.FinalTimeout <= 0 {
		cfg.FinalTimeout = 30 * time.Second
	}
	runCtx, cancel := context.WithCancel(ctx)
	m := &Monitor{nodes: nodes, cfg: cfg, parent: ctx, cancel: cancel, done: make(chan struct{})}
	go m.run(runCtx)
	return m
}

// Stop ends the monitor and returns the first divergence or stall it found.
// Before it returns, it checks agreement up to the height that all nodes have
// committed at that moment, within FinalTimeout. An RPC error in this last
// check is not a failure; only a disagreement is.
func (m *Monitor) Stop() error {
	m.cancel()
	<-m.done
	if err := m.Err(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(m.parent), m.cfg.FinalTimeout)
	defer cancel()
	if heights, reached := m.heights(ctx); len(reached) == len(m.nodes) {
		if err := m.checkUpTo(ctx, m.nodes, commonHeight(heights)); err != nil {
			m.fail(err)
		}
	}
	return m.Err()
}

// Err returns the failure found so far, without stopping the monitor.
func (m *Monitor) Err() error {
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
	lastSeen := make(map[string]time.Time, len(m.nodes))
	for _, n := range m.nodes {
		lastSeen[n.Name()] = lastAdvance
	}
	for {
		heights, reached := m.heights(ctx)
		if ctx.Err() != nil {
			return
		}
		now := time.Now()

		// Liveness: the highest height must advance within StallTimeout.
		var highest int64
		for _, h := range heights {
			highest = max(highest, h)
		}
		if highest > lastHeight {
			lastHeight, lastAdvance = highest, now
		} else if now.Sub(lastAdvance) > m.cfg.StallTimeout {
			m.fail(fmt.Errorf("chain did not advance past height %d in %s: %s",
				lastHeight, m.cfg.StallTimeout, m.formatHeights(heights)))
			return
		}

		// Each node must answer within StallTimeout, so that its heights do
		// not stay unchecked for long.
		for _, n := range m.nodes {
			if _, ok := heights[n.Name()]; ok {
				lastSeen[n.Name()] = now
			} else if now.Sub(lastSeen[n.Name()]) > m.cfg.StallTimeout {
				m.fail(fmt.Errorf("%s did not answer for %s: %s",
					n.Name(), m.cfg.StallTimeout, m.formatHeights(heights)))
				return
			}
		}

		// Agreement: compare all nodes up to their common height. While a
		// node does not answer, wait, so that no height skips that node.
		if len(reached) == len(m.nodes) {
			if err := m.checkUpTo(ctx, m.nodes, commonHeight(heights)); err != nil {
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
// A node that does not answer (for example while it restarts) is left out.
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

// commonHeight returns the lowest height in heights.
func commonHeight(heights map[string]int64) int64 {
	var common int64
	first := true
	for _, h := range heights {
		if first || h < common {
			common, first = h, false
		}
	}
	return common
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
