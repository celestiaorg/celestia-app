package grpc_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v9/fibre/internal/grpc"
	"github.com/celestiaorg/celestia-app/v9/fibre/validator"
	"github.com/celestiaorg/celestia-app/v9/x/valaddr/types"
	core "github.com/cometbft/cometbft/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	clock "github.com/filecoin-project/go-clock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	grpc2 "google.golang.org/grpc"
)

type mockQueryClient struct {
	fibreProviderInfoFn func(context.Context, *types.QueryFibreProviderInfoRequest, ...grpc2.CallOption) (*types.QueryFibreProviderInfoResponse, error)
	allFibreProvidersFn func(context.Context, *types.QueryAllFibreProvidersRequest, ...grpc2.CallOption) (*types.QueryAllFibreProvidersResponse, error)
}

func (m *mockQueryClient) FibreProviderInfo(ctx context.Context, in *types.QueryFibreProviderInfoRequest, opts ...grpc2.CallOption) (*types.QueryFibreProviderInfoResponse, error) {
	if m.fibreProviderInfoFn != nil {
		return m.fibreProviderInfoFn(ctx, in, opts...)
	}
	return nil, errors.New("not implemented")
}

func (m *mockQueryClient) AllFibreProviders(ctx context.Context, in *types.QueryAllFibreProvidersRequest, opts ...grpc2.CallOption) (*types.QueryAllFibreProvidersResponse, error) {
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
				allFibreProvidersFn: func(context.Context, *types.QueryAllFibreProvidersRequest, ...grpc2.CallOption) (*types.QueryAllFibreProvidersResponse, error) {
					return &types.QueryAllFibreProvidersResponse{
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
				allFibreProvidersFn: func(context.Context, *types.QueryAllFibreProvidersRequest, ...grpc2.CallOption) (*types.QueryAllFibreProvidersResponse, error) {
					return &types.QueryAllFibreProvidersResponse{
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
				allFibreProvidersFn: func(context.Context, *types.QueryAllFibreProvidersRequest, ...grpc2.CallOption) (*types.QueryAllFibreProvidersResponse, error) {
					return &types.QueryAllFibreProvidersResponse{
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
				allFibreProvidersFn: func(context.Context, *types.QueryAllFibreProvidersRequest, ...grpc2.CallOption) (*types.QueryAllFibreProvidersResponse, error) {
					return &types.QueryAllFibreProvidersResponse{
						Providers: []types.FibreProvider{{ValidatorConsensusAddress: consAddr, Info: types.FibreProviderInfo{Host: expectedHost}}},
					}, nil
				},
			},
		},
		{
			name: "error",
			mock: &mockQueryClient{
				allFibreProvidersFn: func(context.Context, *types.QueryAllFibreProvidersRequest, ...grpc2.CallOption) (*types.QueryAllFibreProvidersResponse, error) {
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

func TestPullHost(t *testing.T) {
	val := createTestValidator(nil)
	expectedHost := "validator1.example.com:9090"

	tests := []struct {
		name    string
		mock    *mockQueryClient
		want    string
		wantErr string
	}{
		{
			name: "success",
			mock: &mockQueryClient{
				fibreProviderInfoFn: func(context.Context, *types.QueryFibreProviderInfoRequest, ...grpc2.CallOption) (*types.QueryFibreProviderInfoResponse, error) {
					return &types.QueryFibreProviderInfoResponse{Info: &types.FibreProviderInfo{Host: expectedHost}, Found: true}, nil
				},
			},
			want: expectedHost,
		},
		{
			name: "not found",
			mock: &mockQueryClient{
				fibreProviderInfoFn: func(context.Context, *types.QueryFibreProviderInfoRequest, ...grpc2.CallOption) (*types.QueryFibreProviderInfoResponse, error) {
					return &types.QueryFibreProviderInfoResponse{Found: false}, nil
				},
			},
			wantErr: "host not found",
		},
		{
			name: "error",
			mock: &mockQueryClient{
				fibreProviderInfoFn: func(context.Context, *types.QueryFibreProviderInfoRequest, ...grpc2.CallOption) (*types.QueryFibreProviderInfoResponse, error) {
					return nil, errors.New("grpc error")
				},
			},
			wantErr: "grpc error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			host, err := grpc.NewHostRegistry(tt.mock, slog.Default()).PullHost(context.Background(), val)
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

func TestPullHost_OverwritesCache(t *testing.T) {
	val := createTestValidator(nil)
	consAddr := getConsAddrString(val)
	firstHost := "validator1.example.com:9090"
	secondHost := "validator1.example.com:9091"

	registry := grpc.NewHostRegistry(&mockQueryClient{
		allFibreProvidersFn: func(context.Context, *types.QueryAllFibreProvidersRequest, ...grpc2.CallOption) (*types.QueryAllFibreProvidersResponse, error) {
			return &types.QueryAllFibreProvidersResponse{
				Providers: []types.FibreProvider{{ValidatorConsensusAddress: consAddr, Info: types.FibreProviderInfo{Host: firstHost}}},
			}, nil
		},
		fibreProviderInfoFn: func(context.Context, *types.QueryFibreProviderInfoRequest, ...grpc2.CallOption) (*types.QueryFibreProviderInfoResponse, error) {
			return &types.QueryFibreProviderInfoResponse{Info: &types.FibreProviderInfo{Host: secondHost}, Found: true}, nil
		},
	}, slog.Default())
	err := registry.Start(t.Context())
	require.NoError(t, err)

	host, _ := registry.GetHost(context.Background(), val)
	assert.Equal(t, firstHost, host.String())

	host, _ = registry.PullHost(context.Background(), val)
	assert.Equal(t, secondHost, host.String())

	host, _ = registry.GetHost(context.Background(), val)
	assert.Equal(t, secondHost, host.String())
}

func TestHostRegistry_ConcurrentAccess(t *testing.T) {
	val := createTestValidator(nil)
	consAddr := getConsAddrString(val)
	expectedHost := "validator1.example.com:9090"

	var callCount int
	var mu sync.Mutex
	registry := grpc.NewHostRegistry(&mockQueryClient{
		allFibreProvidersFn: func(context.Context, *types.QueryAllFibreProvidersRequest, ...grpc2.CallOption) (*types.QueryAllFibreProvidersResponse, error) {
			mu.Lock()
			callCount++
			mu.Unlock()
			return &types.QueryAllFibreProvidersResponse{
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

func TestRefreshHost(t *testing.T) {
	val := createTestValidator(nil)
	consAddr := getConsAddrString(val)
	const (
		initialHost = "validator1.example.com:9090"
		changedHost = "validator1.example.com:9091"
		invalidHost = "invalid-host-without-port"
	)

	var (
		mu       sync.Mutex
		curHost  = initialHost
		infoCall int
	)
	setHost := func(h string) { mu.Lock(); curHost = h; mu.Unlock() }
	calls := func() int { mu.Lock(); defer mu.Unlock(); return infoCall }

	mock := &mockQueryClient{
		allFibreProvidersFn: func(context.Context, *types.QueryAllFibreProvidersRequest, ...grpc2.CallOption) (*types.QueryAllFibreProvidersResponse, error) {
			return &types.QueryAllFibreProvidersResponse{
				Providers: []types.FibreProvider{{ValidatorConsensusAddress: consAddr, Info: types.FibreProviderInfo{Host: initialHost}}},
			}, nil
		},
		fibreProviderInfoFn: func(context.Context, *types.QueryFibreProviderInfoRequest, ...grpc2.CallOption) (*types.QueryFibreProviderInfoResponse, error) {
			mu.Lock()
			defer mu.Unlock()
			infoCall++
			return &types.QueryFibreProviderInfoResponse{Info: &types.FibreProviderInfo{Host: curHost}, Found: true}, nil
		},
	}

	mockClock := clock.NewMock()
	const interval = time.Minute
	registry := grpc.NewHostRegistry(mock, slog.Default(), grpc.WithClock(mockClock), grpc.WithRefreshInterval(interval))
	require.NoError(t, registry.Start(t.Context())) // seeds initialHost via PullAll

	// 1) host unchanged -> (false, valid).
	changed, valid, err := registry.RefreshHost(t.Context(), val)
	require.NoError(t, err)
	assert.False(t, changed)
	assert.True(t, valid)

	// 2) host changed on chain, but within the interval -> rate-limited, no query.
	setHost(changedHost)
	before := calls()
	changed, _, err = registry.RefreshHost(t.Context(), val)
	require.NoError(t, err)
	assert.False(t, changed)
	assert.Equal(t, before, calls(), "rate-limited refresh must not query")

	// 3) advance past the interval -> detects the valid change and updates cache.
	mockClock.Add(interval)
	changed, valid, err = registry.RefreshHost(t.Context(), val)
	require.NoError(t, err)
	assert.True(t, changed)
	assert.True(t, valid)
	host, err := registry.GetHost(t.Context(), val)
	require.NoError(t, err)
	assert.Equal(t, changedHost, host.String())

	// 4) host changed to an invalid value -> (changed, !valid); cache still updated.
	mockClock.Add(interval)
	setHost(invalidHost)
	changed, valid, err = registry.RefreshHost(t.Context(), val)
	require.NoError(t, err)
	assert.True(t, changed)
	assert.False(t, valid)

	// 5) same invalid host on a later block -> signal goes quiet (no re-fire).
	mockClock.Add(interval)
	changed, _, err = registry.RefreshHost(t.Context(), val)
	require.NoError(t, err)
	assert.False(t, changed, "an unchanged (still invalid) host must not re-fire changed")
}

func TestRefreshHost_QueryError(t *testing.T) {
	val := createTestValidator(nil)

	var infoCall int
	mock := &mockQueryClient{
		fibreProviderInfoFn: func(context.Context, *types.QueryFibreProviderInfoRequest, ...grpc2.CallOption) (*types.QueryFibreProviderInfoResponse, error) {
			infoCall++
			return nil, errors.New("boom")
		},
	}

	mockClock := clock.NewMock()
	const interval = time.Minute
	registry := grpc.NewHostRegistry(mock, slog.Default(), grpc.WithClock(mockClock), grpc.WithRefreshInterval(interval))

	changed, valid, err := registry.RefreshHost(t.Context(), val)
	require.Error(t, err)
	assert.False(t, changed)
	assert.False(t, valid)
	assert.Equal(t, 1, infoCall)

	// The rate-limit timestamp is stamped even on a failed query: a second call
	// within the interval is suppressed.
	_, _, err = registry.RefreshHost(t.Context(), val)
	require.NoError(t, err)
	assert.Equal(t, 1, infoCall, "failed query must still count against the per-block budget")
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
		allFibreProvidersFn: func(context.Context, *types.QueryAllFibreProvidersRequest, ...grpc2.CallOption) (*types.QueryAllFibreProvidersResponse, error) {
			return &types.QueryAllFibreProvidersResponse{Providers: providers}, nil
		},
	}, slog.Default())
	err := registry.Start(t.Context())
	require.NoError(t, err)

	for i, val := range vals {
		host, _ := registry.GetHost(context.Background(), val)
		assert.Equal(t, providers[i].Info.Host, host.String())
	}
}
