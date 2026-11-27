package version

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"cosmossdk.io/log"
	"github.com/cometbft/cometbft/node"
	"github.com/cometbft/cometbft/rpc/core"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

func TestReadConfig(t *testing.T) {
	for _, tc := range []struct {
		name, endpoint, interval string
		wantErr                  bool
	}{
		{name: "disabled", interval: "ignored"},
		{name: "default interval", endpoint: "https://collector.example.com"},
		{name: "custom interval", endpoint: "http://localhost:4318/otel", interval: "30s"},
		{name: "invalid URL", endpoint: "://", wantErr: true},
		{name: "missing scheme", endpoint: "localhost:4318", wantErr: true},
		{name: "missing host", endpoint: "https:///metrics", wantErr: true},
		{name: "query", endpoint: "https://host?key=value", wantErr: true},
		{name: "credentials", endpoint: "https://user:pass@host", wantErr: true},
		{name: "bad interval", endpoint: "https://host", interval: "bad", wantErr: true},
		{name: "zero interval", endpoint: "https://host", interval: "0s", wantErr: true},
		{name: "negative interval", endpoint: "https://host", interval: "-1s", wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := viper.New()
			v.Set("version-reporting.endpoint", tc.endpoint)
			if tc.interval != "" {
				v.Set("version-reporting.interval", tc.interval)
			}
			cfg, err := readConfig(v)
			if tc.wantErr {
				require.Error(t, err)
				// Invalid settings must return before touching the node.
				Run(t.Context(), &node.Node{}, v, log.NewNopLogger())
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.endpoint, cfg.endpoint)
			if tc.interval == "30s" {
				require.Equal(t, 30*time.Second, cfg.interval)
			} else {
				require.Equal(t, time.Minute, cfg.interval)
			}
		})
	}
}

func TestDisabledAndMissingNode(t *testing.T) {
	v := viper.New()
	Run(t.Context(), &node.Node{}, v, log.NewNopLogger())
	v.Set("version-reporting.endpoint", "https://collector.example.com")
	Run(t.Context(), nil, v, log.NewNopLogger())
}

func TestConfigureRPCCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	started, release := make(chan struct{}), make(chan struct{})
	finished := make(chan struct{})
	defer func() { close(release); <-finished }()
	done := make(chan error, 1)
	go func() {
		_, err := configureRPC(ctx, func() (*core.Environment, error) {
			defer close(finished)
			close(started)
			<-release // Simulate a disconnected remote signer's GetPubKey.
			return &core.Environment{}, nil
		}, time.Minute, log.NewNopLogger())
		done <- err
	}()
	<-started
	cancel()
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("reporter shutdown waited for the remote signer")
	}
}

func TestConfigureRPCRetriesTransientFailure(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	want := &core.Environment{}
	attempts := 0
	env, err := configureRPC(ctx, func() (*core.Environment, error) {
		attempts++
		if attempts == 1 {
			return nil, errors.New("remote signer temporarily unavailable")
		}
		return want, nil
	}, time.Millisecond, log.NewNopLogger())
	require.NoError(t, err)
	require.Same(t, want, env)
	require.Equal(t, 2, attempts)
}

func TestConfigureRPCCancellationBetweenRetries(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var attempts atomic.Int32
	done := make(chan error, 1)
	go func() {
		_, err := configureRPC(ctx, func() (*core.Environment, error) {
			attempts.Add(1)
			return nil, errors.New("remote signer unavailable")
		}, time.Hour, log.NewNopLogger())
		done <- err
	}()
	require.Eventually(t, func() bool { return attempts.Load() == 1 }, time.Second, time.Millisecond)
	select {
	case err := <-done:
		t.Fatalf("initialization stopped instead of waiting to retry: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	require.EqualValues(t, 1, attempts.Load(), "failed initialization must wait for the interval")
	cancel()
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("shutdown waited for the retry interval")
	}
}
