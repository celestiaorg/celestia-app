package grpc

import (
	"context"
	"errors"
	"sync"

	core "github.com/cometbft/cometbft/types"
)

// ClientCache caches [Client]s per validator using the provided constructor function.
// TODO(@Wondertan): Needs cleanup strategy, e.g. LRU
type ClientCache struct {
	newClient NewClientFn
	mu        sync.Mutex
	clients   map[string]*clientEntry // keyed by validator address string
}

// clientEntry holds a lazily-initialized [Client].
type clientEntry struct {
	sync.Mutex
	clientCloser Client
	err          error
}

// NewClientCache creates a new [ClientCache] with the given [NewClientFn].
func NewClientCache(newClient NewClientFn, expectedSize int) *ClientCache {
	return &ClientCache{
		newClient: newClient,
		clients:   make(map[string]*clientEntry, expectedSize),
	}
}

// GetClient returns a cached [Client] for the validator, creating one if needed.
// Uses the constructor function provided to [NewClientCache]. Only one dial per validator will occur.
func (cc *ClientCache) GetClient(ctx context.Context, val *core.Validator) (Client, error) {
	addr := val.Address.String()

	cc.mu.Lock()
	entry, ok := cc.clients[addr]
	if !ok {
		entry = &clientEntry{}
		cc.clients[addr] = entry
	}
	cc.mu.Unlock()

	entry.Lock()
	defer entry.Unlock()
	if entry.clientCloser != nil {
		return entry.clientCloser, nil
	}
	if entry.err != nil {
		return nil, entry.err
	}

	entry.clientCloser, entry.err = cc.newClient(ctx, val)
	return entry.clientCloser, entry.err
}

// Close closes all cached [Client]s.
func (cc *ClientCache) Close() (err error) {
	cc.mu.Lock()
	defer cc.mu.Unlock()
	for _, entry := range cc.clients {
		entry.Lock()
		if entry.clientCloser != nil {
			err = errors.Join(err, entry.clientCloser.Close())
		}
		entry.Unlock()
	}
	cc.clients = make(map[string]*clientEntry)
	return err
}
