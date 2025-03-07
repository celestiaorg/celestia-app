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
	testName := "MajorUpgradeToV2"
	numNodes := 4
	upgradeHeight := int64(10)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	scope := fmt.Sprintf("%s_%s", testName, time.Now().Format(timeFormat))
	kn, err := knuu.New(ctx, knuu.Options{
		Scope:        scope,
		ProxyEnabled: true,
	})
	testnet.NoError("failed to initialize Knuu", err)

	kn.HandleStopSignal(ctx)
	logger.Printf("Knuu initialized with scope %s", kn.Scope)

	logger.Println("Creating testnet")
	testNet, err := testnet.New(logger, kn, testnet.Options{})
	testnet.NoError("failed to create testnet", err)

	defer testNet.Cleanup(ctx)

	latestVersion, err := testnet.GetLatestVersion()
	testnet.NoError("failed to get latest version", err)

	logger.Printf("Running %s test with version %s", testName, latestVersion)

	testNet.SetConsensusParams(app.DefaultInitialConsensusParams())

	preloader, err := testNet.NewPreloader()
	testnet.NoError("failed to create preloader", err)

	defer func() { _ = preloader.EmptyImages(ctx) }()
	testnet.NoError("failed to add image", preloader.AddImage(ctx, testnet.DockerImageName(latestVersion)))

	logger.Println("Creating genesis nodes")
	for i := 0; i < numNodes; i++ {
		err := testNet.CreateGenesisNode(ctx, latestVersion, 10000000, upgradeHeight, testnet.DefaultResources, true)
		testnet.NoError("failed to create genesis node", err)
	}

	logger.Println("Creating txsim")
	endpoints, err := testNet.RemoteGRPCEndpoints()
	testnet.NoError("failed to get remote gRPC endpoints", err)
	upgradeSchedule := map[int64]uint64{}
	err = testNet.CreateTxClient(ctx, "txsim", testnet.TxsimVersion, 1, "100-2000", 100, testnet.DefaultResources, endpoints[0], upgradeSchedule)
	testnet.NoError("failed to create tx client", err)

	logger.Println("Setting up testnet")
	testnet.NoError("Failed to setup testnet", testNet.Setup(ctx))
	logger.Println("Starting testnet")
	testnet.NoError("Failed to start testnet", testNet.Start(ctx))

	heightBefore := upgradeHeight - 1
	for i := 0; i < numNodes; i++ {

		client, err := testNet.Node(i).Client()
		testnet.NoError("failed to get client", err)

		testnet.NoError("failed to wait for height", waitForHeight(ctx, client, upgradeHeight, 5*time.Minute))

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

		if err := node.Upgrade(ctx, latestVersion); err != nil {
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
