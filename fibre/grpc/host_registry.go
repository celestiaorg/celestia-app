package grpc

import (
	"context"
	"fmt"
	"net/url"
	"sync"

	"github.com/celestiaorg/celestia-app/v6/fibre/validator"
	"github.com/celestiaorg/celestia-app/v6/x/valaddr/types"
	core "github.com/cometbft/cometbft/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

var _ validator.HostRegistry = &HostRegistry{}

// HostRegistry is a registry of validator hosts. It caches the hosts for validators in the active set.
// It uses the [types.QueryClient] to query the fibre provider information for validators in the active set.
type HostRegistry struct {
	queryClient types.QueryClient
	mu          sync.RWMutex
	cachedHosts map[string]validator.Host
}

func NewHostRegistry(queryClient types.QueryClient) *HostRegistry {
	return &HostRegistry{
		queryClient: queryClient,
		cachedHosts: make(map[string]validator.Host),
	}
}

// Start the host registry by pulling all active fibre providers.
func (g *HostRegistry) Start(ctx context.Context) error {
	return g.PullAll(ctx)
}

// GetHost implements the HostRegistry interface.
func (g *HostRegistry) GetHost(ctx context.Context, val *core.Validator) (validator.Host, error) {
	host, err := g.getHost(ctx, val)
	if err != nil {
		return "", err
	}

	// check if the host is a valid URL
	_, err = url.Parse(host.String())
	if err != nil {
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
	return nil
}

// PullHost pulls the host for a specific validator from the query client and caches it, overwriting any existing cached host.
func (g *HostRegistry) PullHost(ctx context.Context, val *core.Validator) (validator.Host, error) {
	consAddr := sdk.ConsAddress(val.Address.Bytes())
	resp, err := g.queryClient.FibreProviderInfo(ctx, &types.QueryFibreProviderInfoRequest{
		ValidatorConsensusAddress: consAddr.String(),
	})
	if err != nil {
		return "", err
	}
	if !resp.Found {
		return "", fmt.Errorf("host not found for validator %s", consAddr.String())
	}

	host := validator.Host(resp.Info.Host)
	g.writeHost(consAddr.String(), host)

	return host, nil
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
