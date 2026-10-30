package grpc

import (
	"context"
	"errors"
	"sync"
	"time"

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

	// rotateCtx is cancelled by Close to stop in-flight background rotations,
	// and rotateWg lets Close wait for their goroutines to finish.
	rotateCtx    context.Context
	rotateCancel context.CancelFunc
	rotateWg     sync.WaitGroup
}

// clientEntry holds a lazily-initialized [Client].
type clientEntry struct {
	sync.Mutex
	clientCloser Client
	err          error
	closed       bool
	created      time.Time
	// Protected by ClientCache.mu.
	users    int
	retired  bool
	rotating bool
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
	cc.rotateCtx, cc.rotateCancel = context.WithCancel(context.Background())
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
		entry.created = time.Now()
	}
	if entry.err == nil && time.Since(entry.created) > rotateAge() {
		cc.rotate(val, entry)
	}
	return entry, entry.clientCloser, entry.err
}

// rotateAge is when a client is replaced: before the server's connection age
// cap, which is jittered by ±10%, closes it.
func rotateAge() time.Duration {
	return keepAliveMaxConnAge * 8 / 10
}

// readyWaiter is a [Client] that can wait for its connection to be ready.
type readyWaiter interface {
	WaitReady(ctx context.Context) bool
}

// rotate dials a replacement for entry in the background and swaps it in once
// connected, so requests never wait on a new connection. entry is retired and
// closes when its requests finish.
func (cc *ClientCache) rotate(val *core.Validator, entry *clientEntry) {
	addr := val.Address.String()
	cc.mu.Lock()
	if cc.closed || entry.retired || entry.rotating || cc.clients[addr] != entry {
		cc.mu.Unlock()
		return
	}
	entry.rotating = true
	cc.rotateWg.Add(1)
	cc.mu.Unlock()

	go func() {
		defer cc.rotateWg.Done()
		ctx, cancel := context.WithTimeout(cc.rotateCtx, rotateTimeout)
		defer cancel()
		client, err := cc.newClient(ctx, val)
		if err == nil {
			if w, ok := client.(readyWaiter); ok && !w.WaitReady(ctx) {
				_ = client.Close()
				err = errors.New("replacement not ready")
			}
		}
		next := &clientEntry{clientCloser: client, created: time.Now()}

		cc.mu.Lock()
		if err != nil || cc.closed || cc.clients[addr] != entry {
			entry.rotating = false
			cc.mu.Unlock()
			if err == nil {
				_ = client.Close()
			}
			return
		}
		cc.clients[addr] = next
		cc.entries[next] = struct{}{}
		entry.retired = true
		closeOld := entry.users == 0
		cc.mu.Unlock()
		if closeOld {
			_ = entry.close()
			cc.mu.Lock()
			delete(cc.entries, entry)
			cc.mu.Unlock()
		}
	}()
}

// rotateTimeout bounds how long a replacement client may take to connect.
const rotateTimeout = 30 * time.Second

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

// connector is a [Client] that can connect before its first RPC.
type connector interface {
	Connect()
}

// Warm creates clients for vals and starts connecting them, so the first
// requests don't pay for the dial and handshake. It returns once every client
// is created; connections complete in the background.
func (cc *ClientCache) Warm(ctx context.Context, vals []*core.Validator) {
	var wg sync.WaitGroup
	for _, val := range vals {
		wg.Go(func() {
			entry, client, err := cc.acquire(ctx, val)
			if c, ok := client.(connector); ok && err == nil {
				c.Connect()
			}
			cc.release(entry)
		})
	}
	wg.Wait()
}

// Close closes all clients, including retired clients with active requests.
// Concurrent Close calls wait for closure to finish. Subsequent requests return an error.
func (cc *ClientCache) Close() (err error) {
	cc.closeMu.Lock()
	defer cc.closeMu.Unlock()

	cc.mu.Lock()
	cc.closed = true
	cc.mu.Unlock()
	// Stop in-flight background rotations and wait for their goroutines to
	// finish, so no replacement connection is dialed or left open past Close.
	cc.rotateCancel()
	cc.rotateWg.Wait()

	cc.mu.Lock()
	entries := cc.entries
	cc.entries = make(map[*clientEntry]struct{})
	cc.clients = make(map[string]*clientEntry)
	cc.mu.Unlock()
	for entry := range entries {
		err = errors.Join(err, entry.close())
	}
	return err
}
