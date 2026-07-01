package grpc_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v10/fibre/internal/grpc"
	"github.com/celestiaorg/celestia-app/v10/fibre/validator"
	"github.com/celestiaorg/celestia-app/v10/x/valaddr/types"
	core "github.com/cometbft/cometbft/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	clock "github.com/filecoin-project/go-clock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	grpc2 "google.golang.org/grpc"
)

type mockQueryClient struct {
	fibreProviderInfoFn func(context.Context, *types.QueryFibreProviderInfoRequest, ...grpc2.CallOption) (*types.QueryFibreProviderInfoResponse, error)
	allFibreProvidersFn func(context.Context, *types.QueryAllBondedFibreProvidersRequest, ...grpc2.CallOption) (*types.QueryAllBondedFibreProvidersResponse, error)
}

func (m *mockQueryClient) FibreProviderInfo(ctx context.Context, in *types.QueryFibreProviderInfoRequest, opts ...grpc2.CallOption) (*types.QueryFibreProviderInfoResponse, error) {
	if m.fibreProviderInfoFn != nil {
		return m.fibreProviderInfoFn(ctx, in, opts...)
	}
	return nil, errors.New("not implemented")
}

func (m *mockQueryClient) AllBondedFibreProviders(ctx context.Context, in *types.QueryAllBondedFibreProvidersRequest, opts ...grpc2.CallOption) (*types.QueryAllBondedFibreProvidersResponse, error) {
	if m.allFibreProvidersFn != nil {
		return m.allFibreProvidersFn(ctx, in, opts...)
	}
	return nil, errors.New("not implemented")
}

func createTestValidator(address []byte) *core.Validator {
	if address == nil {
		address = []byte("test_validator_addr1")
	}
	return &core.Validator{Address: address, VotingPower: 100}
}

func getConsAddrString(val *core.Validator) string {
	return sdk.ConsAddress(val.Address.Bytes()).String()
}

func TestNewHostRegistry(t *testing.T) {
	registry := grpc.NewHostRegistry(&mockQueryClient{}, slog.Default())
	require.NotNil(t, registry)
	var _ validator.HostRegistry = registry
}

func TestGetHost(t *testing.T) {
	val := createTestValidator(nil)
	consAddr := getConsAddrString(val)
	expectedHost := "validator1.example.com:9090"

	tests := []struct {
		name     string
		mock     *mockQueryClient
		preCache bool
		want     string
		wantErr  string
	}{
		{
			name: "empty cache not found",
			mock: &mockQueryClient{
				fibreProviderInfoFn: func(context.Context, *types.QueryFibreProviderInfoRequest, ...grpc2.CallOption) (*types.QueryFibreProviderInfoResponse, error) {
					return &types.QueryFibreProviderInfoResponse{Info: &types.FibreProviderInfo{Host: "other.com"}, Found: false}, nil
				},
			},
			wantErr: "host not found",
		},
		{
			name: "network error",
			mock: &mockQueryClient{
				fibreProviderInfoFn: func(context.Context, *types.QueryFibreProviderInfoRequest, ...grpc2.CallOption) (*types.QueryFibreProviderInfoResponse, error) {
					return nil, errors.New("network error")
				},
			},
			wantErr: "network error",
		},
		{
			name: "cached",
			mock: &mockQueryClient{
				allFibreProvidersFn: func(context.Context, *types.QueryAllBondedFibreProvidersRequest, ...grpc2.CallOption) (*types.QueryAllBondedFibreProvidersResponse, error) {
					return &types.QueryAllBondedFibreProvidersResponse{
						Providers: []types.FibreProvider{{ValidatorConsensusAddress: consAddr, Info: types.FibreProviderInfo{Host: expectedHost}}},
					}, nil
				},
			},
			preCache: true,
			want:     expectedHost,
		},
		{
			name: "not cached pull success",
			mock: &mockQueryClient{
				allFibreProvidersFn: func(context.Context, *types.QueryAllBondedFibreProvidersRequest, ...grpc2.CallOption) (*types.QueryAllBondedFibreProvidersResponse, error) {
					return &types.QueryAllBondedFibreProvidersResponse{
						Providers: []types.FibreProvider{{ValidatorConsensusAddress: "other", Info: types.FibreProviderInfo{Host: "other.com"}}},
					}, nil
				},
				fibreProviderInfoFn: func(context.Context, *types.QueryFibreProviderInfoRequest, ...grpc2.CallOption) (*types.QueryFibreProviderInfoResponse, error) {
					return &types.QueryFibreProviderInfoResponse{Info: &types.FibreProviderInfo{Host: expectedHost}, Found: true}, nil
				},
			},
			preCache: true,
			want:     expectedHost,
		},
		{
			name: "not cached pull fail",
			mock: &mockQueryClient{
				allFibreProvidersFn: func(context.Context, *types.QueryAllBondedFibreProvidersRequest, ...grpc2.CallOption) (*types.QueryAllBondedFibreProvidersResponse, error) {
					return &types.QueryAllBondedFibreProvidersResponse{
						Providers: []types.FibreProvider{{ValidatorConsensusAddress: "other", Info: types.FibreProviderInfo{Host: "other.com"}}},
					}, nil
				},
				fibreProviderInfoFn: func(context.Context, *types.QueryFibreProviderInfoRequest, ...grpc2.CallOption) (*types.QueryFibreProviderInfoResponse, error) {
					return &types.QueryFibreProviderInfoResponse{Found: false}, nil
				},
			},
			preCache: true,
			wantErr:  "host not found",
		},
		{
			name: "garbage host",
			mock: &mockQueryClient{
				fibreProviderInfoFn: func(context.Context, *types.QueryFibreProviderInfoRequest, ...grpc2.CallOption) (*types.QueryFibreProviderInfoResponse, error) {
					return &types.QueryFibreProviderInfoResponse{Found: true, Info: &types.FibreProviderInfo{Host: "ht!tp://bad"}}, nil
				},
			},
			wantErr: "invalid host",
		},
		{
			// Production failure mode #1: gRPC dialer treats the entire
			// string as the host because it doesn't recognise `http` as
			// a resolver and appends `:443`, yielding "too many colons".
			// Catch it at the registry boundary instead.
			name: "scheme prefix rejected",
			mock: &mockQueryClient{
				fibreProviderInfoFn: func(context.Context, *types.QueryFibreProviderInfoRequest, ...grpc2.CallOption) (*types.QueryFibreProviderInfoResponse, error) {
					return &types.QueryFibreProviderInfoResponse{Found: true, Info: &types.FibreProviderInfo{Host: "http://10.0.0.1:7980"}}, nil
				},
			},
			wantErr: "invalid host",
		},
		{
			name: "dns:/// prefix rejected",
			mock: &mockQueryClient{
				fibreProviderInfoFn: func(context.Context, *types.QueryFibreProviderInfoRequest, ...grpc2.CallOption) (*types.QueryFibreProviderInfoResponse, error) {
					return &types.QueryFibreProviderInfoResponse{Found: true, Info: &types.FibreProviderInfo{Host: "dns:///10.0.0.1:7980"}}, nil
				},
			},
			wantErr: "invalid host",
		},
		{
			name: "bare hostname without port rejected",
			mock: &mockQueryClient{
				fibreProviderInfoFn: func(context.Context, *types.QueryFibreProviderInfoRequest, ...grpc2.CallOption) (*types.QueryFibreProviderInfoResponse, error) {
					return &types.QueryFibreProviderInfoResponse{Found: true, Info: &types.FibreProviderInfo{Host: "validator.example.com"}}, nil
				},
			},
			wantErr: "invalid host",
		},
		{
			name: "host:port accepted",
			mock: &mockQueryClient{
				fibreProviderInfoFn: func(context.Context, *types.QueryFibreProviderInfoRequest, ...grpc2.CallOption) (*types.QueryFibreProviderInfoResponse, error) {
					return &types.QueryFibreProviderInfoResponse{Found: true, Info: &types.FibreProviderInfo{Host: "10.0.0.1:7980"}}, nil
				},
			},
			want: "10.0.0.1:7980",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := grpc.NewHostRegistry(tt.mock, slog.Default())
			if tt.preCache {
				err := registry.Start(context.Background())
				require.NoError(t, err)
			}
			host, err := registry.GetHost(t.Context(), val)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.want, host.String())
			}
		})
	}
}

func TestPullAll(t *testing.T) {
	val := createTestValidator(nil)
	consAddr := getConsAddrString(val)
	expectedHost := "validator1.example.com:9090"

	tests := []struct {
		name    string
		mock    *mockQueryClient
		wantErr bool
	}{
		{
			name: "success",
			mock: &mockQueryClient{
				allFibreProvidersFn: func(context.Context, *types.QueryAllBondedFibreProvidersRequest, ...grpc2.CallOption) (*types.QueryAllBondedFibreProvidersResponse, error) {
					return &types.QueryAllBondedFibreProvidersResponse{
						Providers: []types.FibreProvider{{ValidatorConsensusAddress: consAddr, Info: types.FibreProviderInfo{Host: expectedHost}}},
					}, nil
				},
			},
		},
		{
			name: "error",
			mock: &mockQueryClient{
				allFibreProvidersFn: func(context.Context, *types.QueryAllBondedFibreProvidersRequest, ...grpc2.CallOption) (*types.QueryAllBondedFibreProvidersResponse, error) {
					return nil, errors.New("grpc error")
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := grpc.NewHostRegistry(tt.mock, slog.Default())
			err := registry.PullAll(t.Context())
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				host, _ := registry.GetHost(t.Context(), val)
				assert.Equal(t, expectedHost, host.String())
			}
		})
	}
}

// TestGetHost_RequeriesAfterInterval verifies GetHost serves the warmed host
// within the rate-limit window and re-queries for the freshest host once the
// window elapses.
func TestGetHost_RequeriesAfterInterval(t *testing.T) {
	val := createTestValidator(nil)
	consAddr := getConsAddrString(val)
	const (
		firstHost  = "validator1.example.com:9090"
		secondHost = "validator1.example.com:9091"
	)

	infoCalls := 0
	mock := &mockQueryClient{
		allFibreProvidersFn: func(context.Context, *types.QueryAllBondedFibreProvidersRequest, ...grpc2.CallOption) (*types.QueryAllBondedFibreProvidersResponse, error) {
			return &types.QueryAllBondedFibreProvidersResponse{
				Providers: []types.FibreProvider{{ValidatorConsensusAddress: consAddr, Info: types.FibreProviderInfo{Host: firstHost}}},
			}, nil
		},
		fibreProviderInfoFn: func(context.Context, *types.QueryFibreProviderInfoRequest, ...grpc2.CallOption) (*types.QueryFibreProviderInfoResponse, error) {
			infoCalls++
			return &types.QueryFibreProviderInfoResponse{Info: &types.FibreProviderInfo{Host: secondHost}, Found: true}, nil
		},
	}

	mockClock := clock.NewMock()
	const interval = time.Minute
	registry := grpc.NewHostRegistry(mock, slog.Default(), grpc.WithClock(mockClock), grpc.WithRefreshInterval(interval))
	require.NoError(t, registry.Start(t.Context())) // warms firstHost via PullAll

	// Within the window: served from the warmed host, no per-validator query.
	host, err := registry.GetHost(t.Context(), val)
	require.NoError(t, err)
	assert.Equal(t, firstHost, host.String())
	assert.Equal(t, 0, infoCalls, "a warm host must be served without a query")

	// Still within the window after a small advance: still served warm.
	mockClock.Add(interval / 2)
	host, err = registry.GetHost(t.Context(), val)
	require.NoError(t, err)
	assert.Equal(t, firstHost, host.String())
	assert.Equal(t, 0, infoCalls)

	// Past the window: re-queries and returns the freshest host.
	mockClock.Add(interval)
	host, err = registry.GetHost(t.Context(), val)
	require.NoError(t, err)
	assert.Equal(t, secondHost, host.String())
	assert.Equal(t, 1, infoCalls, "an elapsed window must trigger exactly one re-query")

	// The re-queried host is now cached and served within the new window.
	host, err = registry.GetHost(t.Context(), val)
	require.NoError(t, err)
	assert.Equal(t, secondHost, host.String())
	assert.Equal(t, 1, infoCalls)
}

func TestHostRegistry_ConcurrentAccess(t *testing.T) {
	val := createTestValidator(nil)
	consAddr := getConsAddrString(val)
	expectedHost := "validator1.example.com:9090"

	var callCount int
	var mu sync.Mutex
	registry := grpc.NewHostRegistry(&mockQueryClient{
		allFibreProvidersFn: func(context.Context, *types.QueryAllBondedFibreProvidersRequest, ...grpc2.CallOption) (*types.QueryAllBondedFibreProvidersResponse, error) {
			mu.Lock()
			callCount++
			mu.Unlock()
			return &types.QueryAllBondedFibreProvidersResponse{
				Providers: []types.FibreProvider{{ValidatorConsensusAddress: consAddr, Info: types.FibreProviderInfo{Host: expectedHost}}},
			}, nil
		},
	}, slog.Default())
	err := registry.Start(t.Context())
	require.NoError(t, err)

	var wg sync.WaitGroup
	for range 100 {
		wg.Go(func() {
			host, err := registry.GetHost(t.Context(), val)
			require.NoError(t, err)
			assert.Equal(t, expectedHost, host.String())
		})
	}
	wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	assert.LessOrEqual(t, callCount, 5)
}

// TestGetHost_ServesLastKnownOnQueryError verifies that once the window elapses,
// a transient query failure falls back to the last known host rather than
// failing the caller.
func TestGetHost_ServesLastKnownOnQueryError(t *testing.T) {
	val := createTestValidator(nil)
	consAddr := getConsAddrString(val)
	const warmHost = "validator1.example.com:9090"

	fail := false
	mock := &mockQueryClient{
		allFibreProvidersFn: func(context.Context, *types.QueryAllBondedFibreProvidersRequest, ...grpc2.CallOption) (*types.QueryAllBondedFibreProvidersResponse, error) {
			return &types.QueryAllBondedFibreProvidersResponse{
				Providers: []types.FibreProvider{{ValidatorConsensusAddress: consAddr, Info: types.FibreProviderInfo{Host: warmHost}}},
			}, nil
		},
		fibreProviderInfoFn: func(context.Context, *types.QueryFibreProviderInfoRequest, ...grpc2.CallOption) (*types.QueryFibreProviderInfoResponse, error) {
			if fail {
				return nil, errors.New("boom")
			}
			return &types.QueryFibreProviderInfoResponse{Info: &types.FibreProviderInfo{Host: warmHost}, Found: true}, nil
		},
	}

	mockClock := clock.NewMock()
	const interval = time.Minute
	registry := grpc.NewHostRegistry(mock, slog.Default(), grpc.WithClock(mockClock), grpc.WithRefreshInterval(interval))
	require.NoError(t, registry.Start(t.Context())) // warms warmHost via PullAll

	// Force a re-query past the window, but make the query fail.
	mockClock.Add(interval * 2)
	fail = true
	host, err := registry.GetHost(t.Context(), val)
	require.NoError(t, err)
	assert.Equal(t, warmHost, host.String(), "a transient query failure should serve the last known host")
}

// TestGetHost_QueryError verifies that a query failure with no previously known
// host surfaces the error.
func TestGetHost_QueryError(t *testing.T) {
	val := createTestValidator(nil)

	var infoCall int
	mock := &mockQueryClient{
		fibreProviderInfoFn: func(context.Context, *types.QueryFibreProviderInfoRequest, ...grpc2.CallOption) (*types.QueryFibreProviderInfoResponse, error) {
			infoCall++
			return nil, errors.New("boom")
		},
	}
	registry := grpc.NewHostRegistry(mock, slog.Default())

	_, err := registry.GetHost(t.Context(), val)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "boom")
	assert.Equal(t, 1, infoCall)
}

// TestGetHost_QueryTimeout verifies the on-chain query is bounded by the
// configured timeout, so a hanging state node can't stall the caller.
func TestGetHost_QueryTimeout(t *testing.T) {
	val := createTestValidator(nil)

	mock := &mockQueryClient{
		fibreProviderInfoFn: func(ctx context.Context, _ *types.QueryFibreProviderInfoRequest, _ ...grpc2.CallOption) (*types.QueryFibreProviderInfoResponse, error) {
			<-ctx.Done() // hang until the query context is cancelled by the timeout
			return nil, ctx.Err()
		},
	}

	registry := grpc.NewHostRegistry(mock, slog.Default(), grpc.WithQueryTimeout(50*time.Millisecond))

	done := make(chan struct{})
	var err error
	go func() {
		_, err = registry.GetHost(context.Background(), val)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("GetHost did not return; query timeout was not enforced")
	}

	require.Error(t, err)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestGetHost_MultipleValidators(t *testing.T) {
	vals := make([]*core.Validator, 0, 5)
	providers := make([]types.FibreProvider, 0, 5)

	for i := range 5 {
		val := createTestValidator(fmt.Appendf(nil, "validator%d", i))
		vals = append(vals, val)
		providers = append(providers, types.FibreProvider{
			ValidatorConsensusAddress: getConsAddrString(val),
			Info:                      types.FibreProviderInfo{Host: fmt.Sprintf("validator%d.com:909%d", i, i)},
		})
	}

	registry := grpc.NewHostRegistry(&mockQueryClient{
		allFibreProvidersFn: func(context.Context, *types.QueryAllBondedFibreProvidersRequest, ...grpc2.CallOption) (*types.QueryAllBondedFibreProvidersResponse, error) {
			return &types.QueryAllBondedFibreProvidersResponse{Providers: providers}, nil
		},
	}, slog.Default())
	err := registry.Start(t.Context())
	require.NoError(t, err)

	for i, val := range vals {
		host, _ := registry.GetHost(context.Background(), val)
		assert.Equal(t, providers[i].Info.Host, host.String())
	}
}
