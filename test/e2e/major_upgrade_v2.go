//nolint:staticcheck
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/celestiaorg/celestia-app/v3/app"
	v1 "github.com/celestiaorg/celestia-app/v3/pkg/appconsts/v1"
	v2 "github.com/celestiaorg/celestia-app/v3/pkg/appconsts/v2"
	"github.com/celestiaorg/celestia-app/v3/test/e2e/testnet"
	"github.com/celestiaorg/knuu/pkg/knuu"
	"github.com/tendermint/tendermint/rpc/client/http"
)

func MajorUpgradeToV2(logger *log.Logger) error {
	latestVersion, err := testnet.GetLatestVersion()
	testnet.NoError("failed to get latest version", err)

	logger.Println("Running major upgrade to v2 test", "version", latestVersion)

	numNodes := 4
	upgradeHeight := int64(10)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger.Println("Creating testnet")
	testNet, err := testnet.New("runMajorUpgradeToV2", seed, nil, "test")
	testnet.NoError("failed to create testnet", err)

	defer testNet.Cleanup()

	cparams := app.DefaultInitialConsensusParams()
	cparams.Version.AppVersion = v1.Version

	testNet.SetConsensusParams(cparams)

	preloader, err := knuu.NewPreloader()
	testnet.NoError("failed to create preloader", err)

	defer func() { _ = preloader.EmptyImages() }()
	testnet.NoError("failed to add image", preloader.AddImage(testnet.DockerImageName(latestVersion)))

	logger.Println("Creating genesis nodes")
	for i := 0; i < numNodes; i++ {
		err := testNet.CreateGenesisNode(latestVersion, 10000000, upgradeHeight, testnet.DefaultResources)
		testnet.NoError("failed to create genesis node", err)
	}

	logger.Println("Creating txsim")
	endpoints, err := testNet.RemoteGRPCEndpoints()
	testnet.NoError("failed to get remote gRPC endpoints", err)
	err = testNet.CreateTxClient("txsim", testnet.TxsimVersion, 1, "100-2000", 100, testnet.DefaultResources, endpoints[0])
	testnet.NoError("failed to create tx client", err)

	logger.Println("Setting up testnet")
	testnet.NoError("Failed to setup testnet", testNet.Setup())
	logger.Println("Starting testnet")
	testnet.NoError("Failed to start testnet", testNet.Start())

	heightBefore := upgradeHeight - 1
	for i := 0; i < numNodes; i++ {

		client, err := testNet.Node(i).Client()
		testnet.NoError("failed to get client", err)

		testnet.NoError("failed to wait for height", waitForHeight(ctx, client, upgradeHeight, time.Minute))

		resp, err := client.Header(ctx, &heightBefore)
		testnet.NoError("failed to get header", err)
		logger.Println("Node", i, "is running on version", resp.Header.Version.App)
		if resp.Header.Version.App != v1.Version {
			return fmt.Errorf("version mismatch before upgrade: expected %d, got %d", v1.Version, resp.Header.Version.App)
		}

		resp, err = client.Header(ctx, &upgradeHeight)
		testnet.NoError("failed to get header", err)
		if resp.Header.Version.App != v2.Version {
			return fmt.Errorf("version mismatch after upgrade: expected %d, got %d", v2.Version, resp.Header.Version.App)
		}
	}

	// make all nodes in the network restart and ensure that progress is still made
	for _, node := range testNet.Nodes() {
		client, err := node.Client()
		testnet.NoError("failed to get client", err)

		height, err := getHeight(ctx, client, time.Minute)
		if err != nil {
			return fmt.Errorf("failed to get height: %w", err)
		}

		if err := node.Upgrade(latestVersion); err != nil {
			return fmt.Errorf("failed to restart node: %w", err)
		}

		if err := waitForHeight(ctx, client, height+3, time.Minute); err != nil {
			return fmt.Errorf("failed to wait for height: %w", err)
		}
	}

	return nil
}

func getHeight(ctx context.Context, client *http.HTTP, period time.Duration) (int64, error) {
	timer := time.NewTimer(period)
	ticker := time.NewTicker(100 * time.Millisecond)
	for {
		select {
		case <-timer.C:
			return 0, fmt.Errorf("failed to get height after %.2f seconds", period.Seconds())
		case <-ticker.C:
			status, err := client.Status(ctx)
			if err == nil {
				return status.SyncInfo.LatestBlockHeight, nil
			}
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return 0, err
			}
		}
	}
}

func waitForHeight(ctx context.Context, client *http.HTTP, height int64, period time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, period)
	defer cancel()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for height %d", height)
		case <-ticker.C:
			currentHeight, err := getHeight(ctx, client, period)
			if err != nil {
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
					return err
				}
				continue
			}
			if currentHeight >= height {
				return nil
			}
		}
	}
}
