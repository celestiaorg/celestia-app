package grpc

import (
	"context"
	"sync"

	"google.golang.org/grpc/stats"
)

type connectionBudgetKey struct{}

// connectionBudget tracks response references that gRPC may abandon on disconnect.
// shortcut: stream-reset leaks stay charged until connection close; remove this fallback when gRPC fixes buffer cleanup.
type connectionBudget struct {
	mu       sync.Mutex
	closed   bool
	handlers int
	leases   map[*memoryLease]int32
}

type connectionBudgetStats struct{}

func (connectionBudgetStats) TagConn(ctx context.Context, _ *stats.ConnTagInfo) context.Context {
	return context.WithValue(ctx, connectionBudgetKey{}, &connectionBudget{leases: make(map[*memoryLease]int32)})
}

func (connectionBudgetStats) HandleConn(ctx context.Context, event stats.ConnStats) {
	if _, ok := event.(*stats.ConnEnd); !ok {
		return
	}
	c := ctx.Value(connectionBudgetKey{}).(*connectionBudget)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	c.releaseResponses()
}

func (c *connectionBudget) releaseHandler() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.handlers--
	c.releaseResponses()
}

// releaseResponses requires c.mu and waits until handlers stop retaining the transport.
func (c *connectionBudget) releaseResponses() {
	if !c.closed || c.handlers != 0 {
		return
	}
	for lease, refs := range c.leases {
		lease.releaseRefs(refs)
	}
	clear(c.leases)
}

func (connectionBudgetStats) TagRPC(ctx context.Context, _ *stats.RPCTagInfo) context.Context {
	return ctx
}

func (connectionBudgetStats) HandleRPC(context.Context, stats.RPCStats) {}
