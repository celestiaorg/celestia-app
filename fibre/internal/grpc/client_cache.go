package grpc

import (
	"context"
	"errors"
	"sync"

	"github.com/celestiaorg/celestia-app/v10/fibre/validator"
	core "github.com/cometbft/cometbft/types"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	otelcodes "go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	grpccodes "google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ClientCache caches [Client]s per validator using the provided constructor function.
// TODO(@Wondertan): Needs cleanup strategy, e.g. LRU
type ClientCache struct {
	newClient NewClientFn
	hosts     validator.HostRegistry
	tracer    trace.Tracer
	mu        sync.Mutex
	clients   map[string]*clientEntry // keyed by validator address string
}

// clientEntry holds a lazily-initialized [Client].
type clientEntry struct {
	sync.Mutex
	clientCloser Client
	err          error
}

// ClientCacheOption configures a [ClientCache].
type ClientCacheOption func(*ClientCache)

// WithTracer sets the tracer used to trace [ClientCache.Request]. A nil tracer
// is ignored, leaving the default otel.Tracer("fibre-client") in place — which
// matches the fibre client so request spans nest under the caller's span.
func WithTracer(tracer trace.Tracer) ClientCacheOption {
	return func(cc *ClientCache) {
		if tracer != nil {
			cc.tracer = tracer
		}
	}
}

// NewClientCache creates a new [ClientCache] with the given [NewClientFn]. The
// [validator.HostRegistry] is used by [ClientCache.Request] to re-resolve a
// validator's host when a request fails because the peer is unreachable.
func NewClientCache(newClient NewClientFn, hosts validator.HostRegistry, expectedSize int, opts ...ClientCacheOption) *ClientCache {
	cc := &ClientCache{
		newClient: newClient,
		hosts:     hosts,
		tracer:    otel.Tracer("fibre-client"),
		clients:   make(map[string]*clientEntry, expectedSize),
	}
	for _, opt := range opts {
		opt(cc)
	}
	return cc
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

// Request runs fn against val's cached [Client]. If it fails in a way a changed
// host could explain — a failed dial (e.g. an invalid host) or a transport-level
// gRPC error (an unreachable or timed-out peer) — it re-queries state once
// (rate-limited per validator) for the host. When the host changed to a valid
// new value it evicts the stale client, re-dials, and retries fn exactly once.
// Application-level errors from a reachable server are returned as-is.
func (cc *ClientCache) Request(ctx context.Context, val *core.Validator, fn func(Client) error) error {
	ctx, span := cc.tracer.Start(ctx, "client_cache.request")
	defer span.End()

	client, err := cc.GetClient(ctx, val)
	if err == nil {
		if err = fn(client); err == nil {
			span.SetStatus(otelcodes.Ok, "")
			return nil
		}
	}
	span.RecordError(err)
	span.AddEvent("initial attempt failed")

	// Don't refresh on a cancelled context: the failure is the caller leaving,
	// not a stale host.
	if ctx.Err() != nil {
		return err
	}
	// client == nil means the dial itself failed.
	if client != nil && !isUnreachable(err) {
		span.AddEvent("application error; not refreshing host")
		return err
	}

	span.AddEvent("refreshing host")
	changed, valid, refreshErr := cc.hosts.RefreshHost(ctx, val)
	span.SetAttributes(
		attribute.Bool("host.changed", changed),
		attribute.Bool("host.valid", valid),
	)
	if refreshErr != nil {
		span.RecordError(refreshErr)
	}
	if !changed {
		span.AddEvent("host unchanged; not retrying")
		return err
	}
	// The host changed on chain, so the cached connection points at a stale
	// address whether or not the new host is valid; drop it either way.
	cc.evict(val)
	if !valid {
		span.AddEvent("host changed but invalid; not retrying")
		return err
	}

	client, retryErr := cc.GetClient(ctx, val)
	if retryErr != nil {
		span.RecordError(retryErr)
		span.SetStatus(otelcodes.Error, "re-dial failed")
		return retryErr
	}
	span.AddEvent("retrying against refreshed host")
	if err = fn(client); err != nil {
		span.RecordError(err)
		span.SetStatus(otelcodes.Error, "retry failed")
		return err
	}
	span.SetStatus(otelcodes.Ok, "")
	return nil
}

// isUnreachable reports whether err is a transport-level gRPC error that a
// changed host could explain: the peer was unreachable or timed out, as opposed
// to an application error returned by a reachable server.
func isUnreachable(err error) bool {
	switch status.Code(err) {
	case grpccodes.Unavailable, grpccodes.DeadlineExceeded:
		return true
	default:
		return false
	}
}

// evict closes and removes the cached [Client] for val, so the next
// [ClientCache.GetClient] re-resolves the host and re-dials. A cached dial
// error is cleared as well.
func (cc *ClientCache) evict(val *core.Validator) {
	addr := val.Address.String()

	cc.mu.Lock()
	entry, ok := cc.clients[addr]
	if ok {
		delete(cc.clients, addr)
	}
	cc.mu.Unlock()

	if !ok {
		return
	}
	entry.Lock()
	defer entry.Unlock()
	if entry.clientCloser != nil {
		_ = entry.clientCloser.Close()
	}
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
