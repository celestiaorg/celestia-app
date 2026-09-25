package grpc

import (
	"context"
	"errors"
	"sync"

	core "github.com/cometbft/cometbft/types"
	"go.opentelemetry.io/otel"
	otelcodes "go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	grpccodes "google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ClientCache caches [Client]s per validator using the provided constructor function.
// TODO(@Wondertan): Needs cleanup strategy, e.g. LRU
type ClientCache struct {
	newClient NewClientFn
	tracer    trace.Tracer
	closeMu   sync.Mutex
	mu        sync.Mutex
	clients   map[string]*clientEntry   // keyed by validator address string
	entries   map[*clientEntry]struct{} // includes retired clients with active requests
	closed    bool
}

// clientEntry holds a lazily-initialized [Client].
type clientEntry struct {
	sync.Mutex
	clientCloser Client
	err          error
	closed       bool
	// Protected by ClientCache.mu.
	users   int
	retired bool
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

// NewClientCache creates a new [ClientCache] with the given [NewClientFn].
// [ClientCache.Request] re-resolves a validator's host through newClient when a
// request fails because the peer is unreachable.
func NewClientCache(newClient NewClientFn, expectedSize int, opts ...ClientCacheOption) *ClientCache {
	cc := &ClientCache{
		newClient: newClient,
		tracer:    otel.Tracer("fibre-client"),
		clients:   make(map[string]*clientEntry, expectedSize),
		entries:   make(map[*clientEntry]struct{}, expectedSize),
	}
	for _, opt := range opts {
		opt(cc)
	}
	return cc
}

var errCacheClosed = errors.New("client cache is closed")

// acquire holds a lease even when dialing fails, so eviction can identify the generation.
func (cc *ClientCache) acquire(ctx context.Context, val *core.Validator) (*clientEntry, Client, error) {
	addr := val.Address.String()
	cc.mu.Lock()
	if cc.closed {
		cc.mu.Unlock()
		return nil, nil, errCacheClosed
	}
	entry, ok := cc.clients[addr]
	if !ok {
		entry = &clientEntry{}
		cc.clients[addr] = entry
		cc.entries[entry] = struct{}{}
	}
	entry.users++
	cc.mu.Unlock()

	entry.Lock()
	defer entry.Unlock()
	if entry.closed {
		return entry, nil, errCacheClosed
	}
	if entry.clientCloser == nil && entry.err == nil {
		entry.clientCloser, entry.err = cc.newClient(ctx, val)
	}
	return entry, entry.clientCloser, entry.err
}

func (cc *ClientCache) release(entry *clientEntry) {
	if entry == nil {
		return
	}
	cc.mu.Lock()
	entry.users--
	closeClient := entry.retired && entry.users == 0
	cc.mu.Unlock()
	if closeClient {
		_ = entry.close()
		cc.mu.Lock()
		delete(cc.entries, entry)
		cc.mu.Unlock()
	}
}

func (entry *clientEntry) close() error {
	entry.Lock()
	defer entry.Unlock()
	if entry.closed {
		return nil
	}
	entry.closed = true
	if entry.clientCloser != nil {
		return entry.clientCloser.Close()
	}
	return nil
}

// Request runs fn against val's cached [Client]. If it fails in a way a changed
// host could explain — a failed dial (e.g. an invalid host) or a transport-level
// gRPC error (an unreachable or timed-out peer) — it evicts the stale client and
// re-dials once, retrying fn exactly once. The re-dial resolves the host afresh
// through the [NewClientFn] (rate-limited in the host registry), so a host that
// changed on chain is picked up here. Application-level errors from a reachable
// server are returned as-is.
func (cc *ClientCache) Request(ctx context.Context, val *core.Validator, fn func(Client) error) error {
	ctx, span := cc.tracer.Start(ctx, "client_cache.request")
	defer span.End()

	entry, client, err := cc.acquire(ctx, val)
	if err == nil {
		err = fn(client)
	}
	cc.release(entry)
	if err == nil {
		span.SetStatus(otelcodes.Ok, "")
		return nil
	}
	span.RecordError(err)
	span.AddEvent("initial attempt failed")

	// Don't retry on a cancelled context: the failure is the caller leaving,
	// not a stale host.
	if ctx.Err() != nil {
		return err
	}
	// client == nil means the dial itself failed. Only a failed dial or an
	// unreachable/timed-out peer can be explained by a stale host; an application
	// error from a reachable server is returned as-is.
	if client != nil && !isUnreachable(err) {
		span.AddEvent("application error; not retrying")
		return err
	}

	// Retire the stale connection and re-dial once. acquire re-runs the
	// [NewClientFn], which re-resolves the host, so a host that changed on chain
	// is picked up on the retry.
	span.AddEvent("evicting and re-dialing")
	cc.evict(val, entry)

	entry, client, retryErr := cc.acquire(ctx, val)
	defer cc.release(entry)
	if retryErr != nil {
		span.RecordError(retryErr)
		span.SetStatus(otelcodes.Error, "re-dial failed")
		return retryErr
	}
	span.AddEvent("retrying against re-resolved host")
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

// evict removes only the failed generation, allowing its active requests to finish.
func (cc *ClientCache) evict(val *core.Validator, entry *clientEntry) {
	addr := val.Address.String()
	cc.mu.Lock()
	if entry == nil || cc.clients[addr] != entry {
		cc.mu.Unlock()
		return
	}
	delete(cc.clients, addr)
	entry.retired = true
	closeClient := entry.users == 0
	cc.mu.Unlock()
	if closeClient {
		_ = entry.close()
		cc.mu.Lock()
		delete(cc.entries, entry)
		cc.mu.Unlock()
	}
}

// Close closes all clients, including retired clients with active requests.
// Concurrent Close calls wait for closure to finish. Subsequent requests return an error.
func (cc *ClientCache) Close() (err error) {
	cc.closeMu.Lock()
	defer cc.closeMu.Unlock()

	cc.mu.Lock()
	cc.closed = true
	entries := cc.entries
	cc.entries = make(map[*clientEntry]struct{})
	cc.clients = make(map[string]*clientEntry)
	cc.mu.Unlock()
	for entry := range entries {
		err = errors.Join(err, entry.close())
	}
	return err
}
