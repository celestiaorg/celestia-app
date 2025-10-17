package grpc_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/celestiaorg/celestia-app/v6/fibre/grpc"
	"github.com/celestiaorg/celestia-app/v6/x/fibre/types"
	core "github.com/cometbft/cometbft/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClientCache(t *testing.T) {
	const numGoroutines = 20
	validators := []*core.Validator{
		{
			Address: []byte("validator-1"),
		},
		{
			Address: []byte("validator-2"),
		},
	}
	cache := grpc.NewClientCache(mockClientFn(false), len(validators))

	clients := make([]types.FibreClient, numGoroutines)
	errors := make([]error, numGoroutines)

	// concurrently get clients for multiple validators
	var wg sync.WaitGroup
	wg.Add(numGoroutines)
	for i := range numGoroutines {
		go func(idx int) {
			defer wg.Done()
			// each goroutine requests a client from a validator (round-robin)
			val := validators[idx%len(validators)]
			clients[idx], errors[idx] = cache.GetClient(t.Context(), val)
		}(i)
	}
	wg.Wait()

	// all should succeed
	for i := range numGoroutines {
		require.NoError(t, errors[i])
		require.NotNil(t, clients[i])
	}

	// clients for the same validator should be identical
	for i := range numGoroutines {
		for j := i + 1; j < numGoroutines; j++ {
			if i%len(validators) == j%len(validators) {
				assert.Equal(t, clients[i], clients[j], "clients for same validator should be identical")
			} else {
				assert.NotEqual(t, clients[i], clients[j], "clients for different validators should be different")
			}
		}
	}

	// verify none are closed yet
	mockClient1 := clients[0].(*mockFibreClientCloser)
	mockClient2 := clients[1].(*mockFibreClientCloser)
	assert.False(t, mockClient1.closed)
	assert.False(t, mockClient2.closed)

	// close should succeed and close all clients
	err := cache.Close()
	assert.NoError(t, err)
	assert.True(t, mockClient1.closed)
	assert.True(t, mockClient2.closed)
}

// mockFibreClientCloser is a mock implementation for testing
type mockFibreClientCloser struct {
	types.FibreClient
	closed bool
	id     string // unique identifier for this client
}

func (m *mockFibreClientCloser) Close() error {
	m.closed = true
	return nil
}

// mockClientFn creates a mock grpc.NewClientFn for testing
func mockClientFn(shouldErr bool) grpc.NewClientFn {
	return func(ctx context.Context, val *core.Validator) (grpc.Client, error) {
		if shouldErr {
			return nil, errors.New("mock client creation error")
		}
		return &mockFibreClientCloser{
			id: val.Address.String(), // use validator address as unique id
		}, nil
	}
}
