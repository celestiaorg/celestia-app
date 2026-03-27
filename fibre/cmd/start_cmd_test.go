package main

import (
	"context"
	"io"
	"testing"

	"github.com/celestiaorg/celestia-app/v8/fibre"
	"github.com/celestiaorg/celestia-app/v8/fibre/state"
	"github.com/celestiaorg/celestia-app/v8/fibre/validator"
	core "github.com/cometbft/cometbft/types"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStartCmdConfigPrecedence(t *testing.T) {
	defaults := fibre.DefaultServerConfig()
	tests := []struct {
		name                    string
		fileServerListenAddress string
		fileAppGRPCAddress      string
		args                    []string
		wantServerListenAddress string
		wantAppGRPCAddress      string
	}{
		{
			name:                    "defaults when no file and no flags",
			wantServerListenAddress: defaults.ServerListenAddress,
			wantAppGRPCAddress:      defaults.AppGRPCAddress,
		},
		{
			name:                    "file overrides defaults",
			fileServerListenAddress: "127.0.0.1:8111",
			fileAppGRPCAddress:      "127.0.0.1:9111",
			wantServerListenAddress: "127.0.0.1:8111",
			wantAppGRPCAddress:      "127.0.0.1:9111",
		},
		{
			name:                    "flags override file",
			fileServerListenAddress: "127.0.0.1:8111",
			fileAppGRPCAddress:      "127.0.0.1:9111",
			args: []string{
				"--" + flagServerListenAddress, "127.0.0.1:8222",
				"--" + flagAppGRPCAddress, "127.0.0.1:9222",
			},
			wantServerListenAddress: "127.0.0.1:8222",
			wantAppGRPCAddress:      "127.0.0.1:9222",
		},
		{
			name:                    "partial flag override keeps file value for unset flag",
			fileServerListenAddress: "127.0.0.1:8111",
			fileAppGRPCAddress:      "127.0.0.1:9111",
			args: []string{
				"--" + flagServerListenAddress, "127.0.0.1:8333",
			},
			wantServerListenAddress: "127.0.0.1:8333",
			wantAppGRPCAddress:      "127.0.0.1:9111",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			if tc.fileServerListenAddress != "" || tc.fileAppGRPCAddress != "" {
				writeConfig(t, home, tc.fileServerListenAddress, tc.fileAppGRPCAddress)
			}

			cmd, got := newTestStartCmd(t, home)
			cmd.SetArgs(tc.args)
			require.NoError(t, cmd.ExecuteContext(context.Background()))

			assert.Equal(t, tc.wantServerListenAddress, got.ServerListenAddress)
			assert.Equal(t, tc.wantAppGRPCAddress, got.AppGRPCAddress)
			assert.Equal(t, home, got.Path)
		})
	}
}

// newTestStartCmd creates a start command wired to a temp home dir with a
// capturing run function. The returned pointer holds the ServerConfig that
// RunE receives, so callers can assert on it after execution.
func newTestStartCmd(t *testing.T, home string) (*cobra.Command, *fibre.ServerConfig) {
	t.Helper()

	got := new(fibre.ServerConfig)
	cmd := newStartCmd(func(_ context.Context, cfg fibre.ServerConfig) error {
		*got = cfg
		return nil
	})
	cmd.Flags().String(flagHome, home, "")
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	return cmd, got
}

// TestStartCmdNilLog verifies that the start command does not panic when
// cfg.Log is nil (the default). The command builds a ServerConfig with nil Log,
// which NewServer.Validate populates on its own copy. The fix uses
// server.Config.Log instead of cfg.Log inside startServer.
func TestStartCmdNilLog(t *testing.T) {
	home := t.TempDir()
	mockPV := core.NewMockPV()

	cmd := newStartCmd(func(ctx context.Context, cfg fibre.ServerConfig) error {
		// cfg.Log is nil here, exactly like production.
		require.Nil(t, cfg.Log, "command must not set Log — Validate handles it")

		cfg.ServerListenAddress = "127.0.0.1:0"
		cfg.StateClientFn = func() (state.Client, error) {
			return &stubStateClient{chainID: "test"}, nil
		}
		cfg.SignerFn = func(string) (core.PrivValidator, error) {
			return &mockPV, nil
		}
		cfg.StoreFn = func(scfg fibre.StoreConfig) (*fibre.Store, error) {
			return fibre.NewMemoryStore(scfg), nil
		}
		return startServer(ctx, cfg)
	})
	cmd.Flags().String(flagHome, home, "")
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	// Pre-cancelled context so startServer runs through start → log →
	// stop → log without blocking on signal.NotifyContext.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	require.NotPanics(t, func() {
		_ = cmd.ExecuteContext(ctx)
	})
}

// stubStateClient is a minimal state.Client for testing startServer.
type stubStateClient struct {
	chainID string
}

func (s *stubStateClient) Start(context.Context) error { return nil }
func (s *stubStateClient) Stop(context.Context) error  { return nil }
func (s *stubStateClient) ChainID() string             { return s.chainID }

func (s *stubStateClient) Head(context.Context) (validator.Set, error) {
	return validator.Set{}, nil
}

func (s *stubStateClient) GetByHeight(context.Context, uint64) (validator.Set, error) {
	return validator.Set{}, nil
}

func (s *stubStateClient) GetHost(context.Context, *core.Validator) (validator.Host, error) {
	return "", nil
}

func (s *stubStateClient) VerifyPromise(context.Context, *state.PaymentPromise) (state.VerifiedPromise, error) {
	return state.VerifiedPromise{}, nil
}

func TestStartCmdGRPCSignerFlags(t *testing.T) {
	tests := []struct {
		name            string
		args            []string
		wantGRPCAddress string
	}{
		{
			name:            "default: gRPC signer flag is set",
			wantGRPCAddress: "127.0.0.1:26659",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			cmd, got := newTestStartCmd(t, home)
			cmd.SetArgs(tc.args)
			require.NoError(t, cmd.ExecuteContext(context.Background()))

			assert.Equal(t, tc.wantGRPCAddress, got.SignerGRPCAddress)
		})
	}
}

func writeConfig(t *testing.T, home, serverListenAddress, appGRPCAddress string) {
	t.Helper()

	cfg := fibre.DefaultServerConfig()
	cfg.ServerListenAddress = serverListenAddress
	cfg.AppGRPCAddress = appGRPCAddress
	require.NoError(t, cfg.Save(fibre.DefaultConfigPath(home)))
}
