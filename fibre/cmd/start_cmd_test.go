package main

import (
	"context"
	"io"
	"testing"

	"github.com/celestiaorg/celestia-app/v8/fibre"
	"github.com/celestiaorg/celestia-app/v8/fibre/state"
	"github.com/celestiaorg/celestia-app/v8/fibre/validator"
	"github.com/cometbft/cometbft/crypto"
	"github.com/cometbft/cometbft/crypto/ed25519"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
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

// TestStartServerNilLog verifies that startServer does not panic when
// cfg.Log is nil. NewServer validates the config and sets a default logger on
// its own copy, but the caller's cfg.Log stays nil. The fix uses
// server.Config.Log instead of cfg.Log.
func TestStartServerNilLog(t *testing.T) {
	privKey := ed25519.GenPrivKey()

	cfg := fibre.DefaultServerConfig()
	cfg.Log = nil // explicitly nil — the scenario that used to panic
	cfg.ServerListenAddress = "127.0.0.1:0"
	cfg.StateClientFn = func() (state.Client, error) {
		return &stubStateClient{chainID: "test"}, nil
	}
	cfg.SignerFn = func(string) (core.PrivValidator, error) {
		return &stubPrivValidator{privKey: privKey}, nil
	}
	cfg.StoreFn = func(scfg fibre.StoreConfig) (*fibre.Store, error) {
		return fibre.NewMemoryStore(scfg), nil
	}

	// Use a pre-cancelled context so startServer runs through start → log →
	// stop → log without blocking on signal.NotifyContext.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	require.NotPanics(t, func() {
		// An error is expected (e.g. from the cancelled context propagation),
		// but a nil-pointer panic is the bug we are guarding against.
		_ = startServer(ctx, cfg)
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

// stubPrivValidator is a minimal core.PrivValidator for testing startServer.
type stubPrivValidator struct {
	privKey ed25519.PrivKey
}

func (s *stubPrivValidator) GetPubKey() (crypto.PubKey, error) {
	return s.privKey.PubKey(), nil
}
func (s *stubPrivValidator) SignVote(string, *cmtproto.Vote) error         { return nil }
func (s *stubPrivValidator) SignProposal(string, *cmtproto.Proposal) error { return nil }
func (s *stubPrivValidator) SignRawBytes(string, string, []byte) ([]byte, error) {
	return nil, nil
}

func writeConfig(t *testing.T, home, serverListenAddress, appGRPCAddress string) {
	t.Helper()

	cfg := fibre.DefaultServerConfig()
	cfg.ServerListenAddress = serverListenAddress
	cfg.AppGRPCAddress = appGRPCAddress
	require.NoError(t, cfg.Save(fibre.DefaultConfigPath(home)))
}
