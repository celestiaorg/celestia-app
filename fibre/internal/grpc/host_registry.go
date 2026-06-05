package grpc

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/celestiaorg/celestia-app/v9/fibre/validator"
	"github.com/celestiaorg/celestia-app/v9/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v9/x/valaddr/types"
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
// [HostRegistry.RefreshHost]. It matches the client's default RPCTimeout so a
// slow or black-holed state node can't add unbounded latency to the request
// that triggered the refresh.
const DefaultQueryTimeout = 15 * time.Second

// HostRegistry is a registry of validator hosts. It caches the hosts for validators in the active set.
// It uses the [types.QueryClient] to query the fibre provider information for validators in the active set.
type HostRegistry struct {
	queryClient types.QueryClient
	log         *slog.Logger
	mu          sync.RWMutex
	cachedHosts map[string]validator.Host

	// Refresh timing state used to rate-limit on-chain host re-queries per validator.
	clock           clock.Clock
	refreshInterval time.Duration
	lastRefresh     map[string]time.Time
	// queryTimeout bounds a single on-chain host query in RefreshHost.
	queryTimeout time.Duration
}

// HostRegistryOption configures a [HostRegistry].
type HostRegistryOption func(*HostRegistry)

// WithClock sets the clock used to rate-limit [HostRegistry.RefreshHost].
func WithClock(c clock.Clock) HostRegistryOption {
	return func(g *HostRegistry) { g.clock = c }
}

// WithRefreshInterval sets the minimum time between on-chain host re-queries
// for a single validator in [HostRegistry.RefreshHost]. A non-positive value
// leaves the default in place.
func WithRefreshInterval(d time.Duration) HostRegistryOption {
	return func(g *HostRegistry) {
		if d > 0 {
			g.refreshInterval = d
		}
	}
}

// WithQueryTimeout sets the timeout bounding a single on-chain host query in
// [HostRegistry.RefreshHost]. A non-positive value leaves the default in place.
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
		cachedHosts:     make(map[string]validator.Host),
		lastRefresh:     make(map[string]time.Time),
		queryTimeout:    DefaultQueryTimeout,
	}
	for _, opt := range opts {
		opt(g)
	}
	return g
}

// Start the host registry by pulling all active fibre providers.
func (g *HostRegistry) Start(ctx context.Context) error {
	return g.PullAll(ctx)
}

// GetHost implements the HostRegistry interface.
//
// The same `host:port` validation that x/valaddr's MsgSetFibreProviderInfo
// applies on registration is re-applied here so that legacy registrations
// (or any future bug that lets a malformed host slip past the chain check)
// surface a clear "got invalid host" error at the registry boundary
// instead of failing later inside grpc.NewClient with a confusing
// "too many colons in address".
func (g *HostRegistry) GetHost(ctx context.Context, val *core.Validator) (validator.Host, error) {
	host, err := g.getHost(ctx, val)
	if err != nil {
		return "", err
	}

	if err := types.ValidateHost(host.String()); err != nil {
		return "", fmt.Errorf("got invalid host %s: %w", host.String(), err)
	}
	return host, nil
}

func (g *HostRegistry) getHost(ctx context.Context, val *core.Validator) (validator.Host, error) {
	valConAddr := sdk.ConsAddress(val.Address.Bytes()).String()

	// check the cache first
	if host, ok := g.readHost(valConAddr); ok {
		return host, nil
	}

	// look up the specific validator's host if it's missing from the cache. It might have
	// been added to the active set since the last refresh.
	return g.PullHost(ctx, val)
}

// PullAll pulls all active fibre providers from the query client and caches them, overwriting any existing cached hosts.
func (g *HostRegistry) PullAll(ctx context.Context) error {
	resp, err := g.queryClient.AllFibreProviders(ctx, &types.QueryAllFibreProvidersRequest{})
	if err != nil {
		return err
	}

	g.mu.Lock()
	defer g.mu.Unlock()
	for _, provider := range resp.Providers {
		g.cachedHosts[provider.ValidatorConsensusAddress] = validator.Host(provider.Info.Host)
	}
	g.log.Info("loaded fibre providers", "count", len(resp.Providers))
	return nil
}

// PullHost pulls the host for a specific validator from the query client and caches it, overwriting any existing cached host.
func (g *HostRegistry) PullHost(ctx context.Context, val *core.Validator) (validator.Host, error) {
	consAddr := sdk.ConsAddress(val.Address.Bytes())
	host, err := g.queryHost(ctx, consAddr.String())
	if err != nil {
		return "", err
	}

	g.writeHost(consAddr.String(), host)
	g.log.Debug("pulled fibre provider host", "validator", consAddr.String(), "host", host)

	return host, nil
}

// RefreshHost implements the [validator.HostRegistry] interface. See the
// interface for the returned semantics.
func (g *HostRegistry) RefreshHost(ctx context.Context, val *core.Validator) (changed bool, isValid bool, err error) {
	consAddr := sdk.ConsAddress(val.Address.Bytes()).String()

	// rate-limit per validator: state cannot change faster than one block, so
	// re-querying more often is pointless. The timestamp is stamped upfront,
	// regardless of the query outcome, to bound query load unconditionally.
	g.mu.Lock()
	if last, ok := g.lastRefresh[consAddr]; ok && g.clock.Since(last) < g.refreshInterval {
		g.mu.Unlock()
		// Rate-limited: skip the query and report no change. changed=false makes
		// the caller fall back to the original error, isValid is meaningless
		// without a fresh query, and err=nil because being rate-limited is not a
		// failure — the host simply hasn't been re-checked this block.
		return false, false, nil
	}
	old := g.cachedHosts[consAddr]
	g.lastRefresh[consAddr] = g.clock.Now()
	g.mu.Unlock()

	// Bound the query so a slow or black-holed state node can't add unbounded
	// latency to the request that triggered the refresh.
	qctx, cancel := context.WithTimeout(ctx, g.queryTimeout)
	defer cancel()
	host, err := g.queryHost(qctx, consAddr)
	if err != nil {
		return false, false, err
	}

	changed = host != old
	isValid = types.ValidateHost(host.String()) == nil
	// Cache the true on-chain host even when invalid, so the next refresh sees
	// new == old and the "changed" signal goes quiet instead of re-firing every
	// block.
	if changed {
		g.writeHost(consAddr, host)
		g.log.Debug("refreshed fibre provider host", "validator", consAddr, "host", host, "valid", isValid)
	}

	return changed, isValid, nil
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

// readHost reads a host from the cache with a read lock.
func (g *HostRegistry) readHost(key string) (validator.Host, bool) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	host, ok := g.cachedHosts[key]
	return host, ok
}

// writeHost writes a single host to the cache with a write lock.
func (g *HostRegistry) writeHost(key string, host validator.Host) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.cachedHosts[key] = host
}
