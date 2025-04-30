package docker_e2e

import (
	"context"
	"fmt"
	"github.com/celestiaorg/go-square/v2/share"
	"github.com/chatton/interchaintest/chain/cosmos"
	"github.com/chatton/interchaintest/dockerutil"
	"github.com/docker/docker/api/types"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"testing"
	"time"
)

func createTxSim(t *testing.T, err error, ctx context.Context, client *client.Client, network string, logger *zap.Logger, cosmosChain *cosmos.Chain) {
	networkName, err := GetNetworkNameFromID(ctx, client, network)
	require.NoError(t, err)

	// Deploy txsim image
	t.Log("Deploying txsim image")
	txsimImage := dockerutil.NewImage(logger, client, networkName, t.Name(), "ghcr.io/celestiaorg/txsim", "v4.0.0-rc1")

	// Get the RPC address to connect to the Celestia node
	rpcAddress := cosmosChain.GetHostRPCAddress()
	t.Logf("Connecting to Celestia node at %s", rpcAddress)

	// Run the txsim container
	opts := dockerutil.ContainerOptions{
		User: dockerutil.GetRootUserString(),
		// Mount the Celestia home directory into the txsim container
		Binds: []string{cosmosChain.Validators[0].VolumeName + ":/celestia-home"},
	}

	t.Logf("waiting for grpc service to be online")
	time.Sleep(10 * time.Second)

	args := []string{
		"/bin/txsim",
		"--key-path", "/celestia-home",
		"--grpc-endpoint", cosmosChain.GetGRPCAddress(),
		"--poll-time", "1s",
		"--seed", "42",
		"--blob", "10",
		"--blob-amounts", "100",
		"--blob-sizes", "100-2000",
		"--gas-price", "0.025",
		"--blob-share-version", fmt.Sprintf("%d", share.ShareVersionZero),
	}

	// Start the txsim container
	container, err := txsimImage.Start(ctx, args, opts)
	require.NoError(t, err, "Failed to start txsim container")
	t.Log("TxSim container started successfully")
	t.Logf("TxSim container ID: %s", container.Name)

	// Wait for a short time to allow txsim to start
	time.Sleep(10 * time.Second)

	// Cleanup the container when the test is done
	t.Cleanup(func() {
		if err := container.Stop(10 * time.Second); err != nil {
			t.Logf("Error stopping txsim container: %v", err)
		}
	})
}

// GetNetworkNameFromID resolves the network name given its ID.
func GetNetworkNameFromID(ctx context.Context, cli *client.Client, networkID string) (string, error) {
	network, err := cli.NetworkInspect(ctx, networkID, types.NetworkInspectOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to inspect network %s: %w", networkID, err)
	}
	if network.Name == "" {
		return "", fmt.Errorf("network %s has no name", networkID)
	}
	return network.Name, nil
}
