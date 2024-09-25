//nolint:staticcheck
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/celestiaorg/celestia-app/v3/app"
	v2 "github.com/celestiaorg/celestia-app/v3/pkg/appconsts/v2"
	v3 "github.com/celestiaorg/celestia-app/v3/pkg/appconsts/v3"
	"github.com/celestiaorg/celestia-app/v3/test/e2e/testnet"
)

func MajorUpgradeToV3(logger *log.Logger) error {
	numNodes := 4
	upgradeHeight := int64(10)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger.Println("Creating testnet")
	testNet, err := testnet.New(ctx, "runMajorUpgradeToV3", seed, nil, "test")
	testnet.NoError("failed to create testnet", err)

	defer testNet.Cleanup(ctx)

	latestVersion, err := testnet.GetLatestVersion()
	testnet.NoError("failed to get latest version", err)

	logger.Println("Running major upgrade to v3 test", "version", latestVersion)

	cp := app.DefaultConsensusParams()
	cp.Version.AppVersion = v2.Version
	testNet.SetConsensusParams(cp)

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
	endpoints, err := testNet.RemoteGRPCEndpoints(ctx)
	testnet.NoError("failed to get remote gRPC endpoints", err)
	upgradeSchedule := map[int64]uint64{
		upgradeHeight: v3.Version,
	}
	err = testNet.CreateTxClient(ctx, "txsim", testnet.TxsimVersion, 1, "100-2000", 100, testnet.DefaultResources, endpoints[0], upgradeSchedule)
	testnet.NoError("failed to create tx client", err)

	logger.Println("Setting up testnet")
	testnet.NoError("Failed to setup testnet", testNet.Setup(ctx))
	logger.Println("Starting testnet")
	testnet.NoError("Failed to start testnet", testNet.Start(ctx))

	for _, node := range testNet.Nodes() {
		client, err := node.Client()
		testnet.NoError("failed to get client", err)
		timer := time.NewTimer(time.Minute)
		ticker := time.NewTicker(3 * time.Second)

		upgradeComplete := false
		for !upgradeComplete {
			lastHeight := int64(0)
			select {
			case <-timer.C:
				return fmt.Errorf("failed to upgrade to v3, last height: %d", lastHeight)
			case <-ticker.C:
				resp, err := client.Header(ctx, nil)
				testnet.NoError("failed to get header", err)
				if resp.Header.Version.App == v3.Version {
					upgradeComplete = true
				}
				lastHeight = resp.Header.Height
			}
		}
		timer.Stop()
		ticker.Stop()
	}

	return nil
}
