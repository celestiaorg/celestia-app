package testnode

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cosmossdk.io/log"
	"github.com/celestiaorg/celestia-app/v7/test/util/genesis"
)

// NewNetwork starts a single validator celestia-app network using the provided
// configurations. Configured accounts will be funded and their keys can be
// accessed in keyring returned client.Context. All rpc, p2p, and grpc addresses
// in the provided configs are overwritten to use open ports. The node can be
// accessed via the returned client.Context or via the returned rpc and grpc
// addresses. Configured genesis options will be applied after all accounts have
// been initialized.
func NewNetwork(t testing.TB, config *Config) (cctx Context, rpcAddr, grpcAddr string) {
	return NewNetworkWithRetry(t, config, 3)
}

// NewNetworkWithRetry creates a testnode network with port retry logic
func NewNetworkWithRetry(t testing.TB, config *Config, maxRetries int) (cctx Context, rpcAddr, grpcAddr string) {
	t.Helper()

	for attempt := range maxRetries {
		result, rpc, grpc, cleanup, err := tryStartNetwork(t, config)
		if err != nil {
			if isPortBindingError(err) {
				if cleanup != nil {
					cleanup()
				}
				time.Sleep(time.Second)
				config.TmConfig.RPC.ListenAddress = fmt.Sprintf("tcp://127.0.0.1:%d", MustGetFreePort())
				config.TmConfig.P2P.ListenAddress = fmt.Sprintf("tcp://127.0.0.1:%d", MustGetFreePort())
				config.TmConfig.RPC.GRPCListenAddress = fmt.Sprintf("tcp://127.0.0.1:%d", MustGetFreePort())
				config.AppConfig.GRPC.Address = fmt.Sprintf("127.0.0.1:%d", MustGetFreePort())
				config.AppConfig.API.Enable = true
				config.AppConfig.API.Address = fmt.Sprintf("tcp://127.0.0.1:%d", MustGetFreePort())
				continue
			}
			t.Fatalf("Failed to start network after %d attempts: %v", attempt+1, err)
		}
		t.Cleanup(cleanup)
		return result, rpc, grpc
	}
	t.Fatalf("Failed to start network after %d attempts", maxRetries)
	return Context{}, "", "" // Never reached
}

// tryStartNetwork attempts to start the network once
func tryStartNetwork(t testing.TB, config *Config) (cctx Context, rpcAddr, grpcAddr string, cleanup func(), err error) {
	t.Helper()

	// initialize the genesis file and validator files for the first validator.
	baseDir := filepath.Join(t.TempDir(), "testnode")
	if err = genesis.InitFiles(baseDir, config.TmConfig, config.AppConfig, config.Genesis, 0); err != nil {
		return Context{}, "", "", nil, err
	}

	tmNode, app, err := NewCometNode(baseDir, &config.UniversalTestingConfig)
	if err != nil {
		return Context{}, "", "", nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	cctx = NewContext(ctx, config.Genesis.Keyring(), config.TmConfig, config.Genesis.ChainID, config.AppConfig.API.Address)
	cctx.tmNode = tmNode

	cctx, stopNode, err := StartNode(tmNode, cctx)
	if err != nil {
		return Context{}, "", "", cleanup, err
	}

	coreEnv, err := tmNode.ConfigureRPC()
	if err != nil {
		return Context{}, "", "", cleanup, err
	}

	grpcServer, cctx, cleanupGRPC, err := StartGRPCServer(log.NewTestLogger(t), app, config.AppConfig, cctx, coreEnv)
	if err != nil {
		return Context{}, "", "", cleanup, err
	}

	apiServer, err := StartAPIServer(app, *config.AppConfig, cctx, grpcServer)
	if err != nil {
		return Context{}, "", "", cleanup, err
	}

	cleanup = func() {
		t.Log("tearing down testnode")
		err := stopNode()
		if err != nil {
			// the test has already completed so log the error instead of
			// failing the test.
			t.Logf("error stopping node %v", err)
		}
		err = cleanupGRPC()
		if err != nil {
			// the test has already completed so just log the error instead of
			// failing the test.
			t.Logf("error when cleaning up GRPC %v", err)
		}
		err = apiServer.Close()
		if err != nil {
			// the test has already completed so just log the error instead of
			// failing the test.
			t.Logf("error when closing API server %v", err)
		}
	}

	return cctx, config.TmConfig.RPC.ListenAddress, config.AppConfig.GRPC.Address, cleanup, nil
}

// isPortBindingError checks if an error is related to port binding failures
func isPortBindingError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	// Check for common port binding error patterns
	return strings.Contains(errStr, "bind: address already in use") ||
		strings.Contains(errStr, "address already in use") ||
		strings.Contains(errStr, "failed to listen on")
}
