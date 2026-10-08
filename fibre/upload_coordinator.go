package fibre

import (
	"context"
	"sync"
)

// uploadCoordinator excludes only identical promises. Its mutex protects the
// owner map and is never held during storage or while waiting for another owner.
type uploadCoordinator struct {
	mu     sync.Mutex
	owners map[string]chan struct{}
}

func (c *uploadCoordinator) acquire(ctx context.Context, hash []byte) (func(), error) {
	key := string(hash)
	for {
		c.mu.Lock()
		if err := ctx.Err(); err != nil {
			c.mu.Unlock()
			return nil, err
		}
		if pending, ok := c.owners[key]; ok {
			c.mu.Unlock()
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-pending:
				continue
			}
		}
		if c.owners == nil {
			c.owners = make(map[string]chan struct{})
		}
		done := make(chan struct{})
		c.owners[key] = done
		c.mu.Unlock()
		return func() {
			c.mu.Lock()
			delete(c.owners, key)
			close(done)
			c.mu.Unlock()
		}, nil
	}
}
