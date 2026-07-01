package grpc

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/celestiaorg/celestia-app/v10/fibre/validator"
	"github.com/celestiaorg/celestia-app/v10/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v10/x/valaddr/types"
	core "github.com/cometbft/cometbft/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/filecoin-project/go-clock"
)

var _ validator.HostRegistry = &HostRegistry{}

// DefaultRefreshInterval is the minimum time between on-chain host re-queries
// for a single validator. It matches the expected block time, since registry
// state cannot change faster than one block.
var DefaultRefreshInterval = appconsts.DelayedPrecommitTimeout + appconsts.TimeoutCommit

// DefaultQueryTimeout bounds a single on-chain host query in
// [HostRegistry.GetHost]. It matches the client's default RPCTimeout so a slow
// or black-holed state node can't add unbounded latency to the request that
// triggered the resolution.
const DefaultQueryTimeout = 15 * time.Second

// HostRegistry resolves validator hosts from on-chain fibre provider state.
//
// GetHost always resolves the freshest address, but re-queries state at most
// once per validator per refreshInterval; within that window it serves the last
// known host. The rate limit — not a real cache — is what bounds query load,
// since provider state cannot change faster than one block. Start warms the last
// known hosts in a single bulk query so the first block of traffic is served
// without a per-validator query storm.
type HostRegistry struct {
	queryClient types.QueryClient
	log         *slog.Logger
	mu          sync.RWMutex
	// lastHosts is the last host resolved per validator consensus address. It is
	// the rate-limiter's fallback within a window and the warm-up target, not a
	// standalone cache.
	lastHosts map[string]validator.Host

	// Rate-limit state for on-chain host re-queries per validator.
	clock           clock.Clock
	refreshInterval time.Duration
	lastRefresh     map[string]time.Time
	// queryTimeout bounds a single on-chain host query in GetHost.
	queryTimeout time.Duration
}

// HostRegistryOption configures a [HostRegistry].
type HostRegistryOption func(*HostRegistry)

// WithClock sets the clock used to rate-limit host re-queries in
// [HostRegistry.GetHost].
func WithClock(c clock.Clock) HostRegistryOption {
	return func(g *HostRegistry) { g.clock = c }
}

// WithRefreshInterval sets the minimum time between on-chain host re-queries
// for a single validator in [HostRegistry.GetHost]. A non-positive value
// leaves the default in place.
func WithRefreshInterval(d time.Duration) HostRegistryOption {
	return func(g *HostRegistry) {
		if d > 0 {
			g.refreshInterval = d
		}
	}
}

// WithQueryTimeout sets the timeout bounding a single on-chain host query in
// [HostRegistry.GetHost]. A non-positive value leaves the default in place.
func WithQueryTimeout(d time.Duration) HostRegistryOption {
	return func(g *HostRegistry) {
		if d > 0 {
			g.queryTimeout = d
		}
	}
}

func NewHostRegistry(queryClient types.QueryClient, log *slog.Logger, opts ...HostRegistryOption) *HostRegistry {
	g := &HostRegistry{
		queryClient:     queryClient,
		log:             log,
		clock:           clock.New(),
		refreshInterval: DefaultRefreshInterval,
		lastHosts:       make(map[string]validator.Host),
		lastRefresh:     make(map[string]time.Time),
		queryTimeout:    DefaultQueryTimeout,
	}
	for _, opt := range opts {
		opt(g)
	}
	return g
}

// Start warms the registry by pulling all active fibre providers in a single
// bulk query, so the first block of traffic is served without a per-validator
// query storm.
func (g *HostRegistry) Start(ctx context.Context) error {
	return g.PullAll(ctx)
}

// GetHost implements the [validator.HostRegistry] interface. It resolves the
// freshest on-chain host for val, re-querying state at most once per validator
// per refresh interval and serving the last known host within that window.
//
// The same `host:port` validation that x/valaddr's MsgSetFibreProviderInfo
// applies on registration is re-applied here so that legacy registrations
// (or any future bug that lets a malformed host slip past the chain check)
// surface a clear "got invalid host" error at the registry boundary
// instead of failing later inside grpc.NewClient with a confusing
// "too many colons in address".
func (g *HostRegistry) GetHost(ctx context.Context, val *core.Validator) (validator.Host, error) {
	host, err := g.resolveHost(ctx, val)
	if err != nil {
		return "", err
	}

	if err := types.ValidateHost(host.String()); err != nil {
		return "", fmt.Errorf("got invalid host %s: %w", host.String(), err)
	}
	return host, nil
}

// resolveHost returns the freshest host for val, respecting the per-validator
// rate limit. Within the refresh window it returns the last known host without
// querying; otherwise it re-queries state, updating the last known host on
// success and falling back to it on a transient query failure.
func (g *HostRegistry) resolveHost(ctx context.Context, val *core.Validator) (validator.Host, error) {
	consAddr := sdk.ConsAddress(val.Address.Bytes()).String()

	// Within the rate-limit window, serve the last known host without querying:
	// provider state can't change faster than one block, so the window bounds
	// query load while still returning a usable address.
	g.mu.Lock()
	last, resolved := g.lastRefresh[consAddr]
	fallback, hasFallback := g.lastHosts[consAddr]
	if hasFallback && resolved && g.clock.Since(last) < g.refreshInterval {
		g.mu.Unlock()
		return fallback, nil
	}
	// Open (or renew) the window now, so that once this query succeeds the
	// subsequent dials for the same validator are served from the last known host
	// instead of re-querying every time.
	g.lastRefresh[consAddr] = g.clock.Now()
	g.mu.Unlock()

	// Bound the query so a slow or black-holed state node can't add unbounded
	// latency to the caller.
	ctx, cancel := context.WithTimeout(ctx, g.queryTimeout)
	defer cancel()
	host, err := g.queryHost(ctx, consAddr)
	if err != nil {
		// Fall back to the last known host on a transient query failure rather
		// than failing the caller; the window will trigger another attempt.
		if hasFallback {
			g.log.Debug("host query failed; serving last known host", "validator", consAddr, "err", err)
			return fallback, nil
		}
		return "", err
	}

	g.writeHost(consAddr, host)
	g.log.Debug("resolved fibre provider host", "validator", consAddr, "host", host)
	return host, nil
}

// PullAll pulls all active fibre providers from the query client and records
// them as the last known hosts, overwriting any existing entries. It stamps the
// refresh timestamp for each so the warmed hosts are served for one refresh
// window before the first per-validator re-query.
func (g *HostRegistry) PullAll(ctx context.Context) error {
	resp, err := g.queryClient.AllBondedFibreProviders(ctx, &types.QueryAllBondedFibreProvidersRequest{})
	if err != nil {
		return err
	}

	g.mu.Lock()
	defer g.mu.Unlock()
	now := g.clock.Now()
	for _, provider := range resp.Providers {
		g.lastHosts[provider.ValidatorConsensusAddress] = validator.Host(provider.Info.Host)
		g.lastRefresh[provider.ValidatorConsensusAddress] = now
	}
	g.log.Info("loaded fibre providers", "count", len(resp.Providers))
	return nil
}

// queryHost queries the fibre provider host for a validator consensus address.
func (g *HostRegistry) queryHost(ctx context.Context, consAddr string) (validator.Host, error) {
	resp, err := g.queryClient.FibreProviderInfo(ctx, &types.QueryFibreProviderInfoRequest{
		ValidatorConsensusAddress: consAddr,
	})
	if err != nil {
		return "", err
	}
	if !resp.Found {
		return "", fmt.Errorf("host not found for validator %s", consAddr)
	}
	return validator.Host(resp.Info.Host), nil
}

// writeHost records a single last known host under a write lock.
func (g *HostRegistry) writeHost(key string, host validator.Host) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.lastHosts[key] = host
}
