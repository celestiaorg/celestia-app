package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/celestiaorg/celestia-app/v3/app"
	"github.com/celestiaorg/celestia-app/v3/pkg/appconsts"
	v2 "github.com/celestiaorg/celestia-app/v3/pkg/appconsts/v2"
	v3 "github.com/celestiaorg/celestia-app/v3/pkg/appconsts/v3"
	"github.com/celestiaorg/celestia-app/v3/test/e2e/testnet"
	zlog "github.com/rs/zerolog/log"
)

func MajorUpgradeToV3(logger *log.Logger) error {
	numNodes := 4
	upgradeHeightV3 := int64(4)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger.Println("Creating testnet")
	testNet, err := testnet.New(ctx, "MajorUpgradeToV3", seed, nil, "test")
	testnet.NoError("failed to create testnet", err)

	defer testNet.Cleanup(ctx)

	// HACKHACK: use a version of celestia-app built from a commit on this PR.
	// This can be removed after the PR is merged to main and we override the
	// upgrade height delay to one block in a new Docker image.
	version := "pr-3882"

	logger.Println("Running major upgrade to v3 test", "version", version)

	consensusParams := app.DefaultConsensusParams()
	consensusParams.Version.AppVersion = v2.Version // Start the test on v2
	testNet.SetConsensusParams(consensusParams)

	preloader, err := testNet.NewPreloader()
	testnet.NoError("failed to create preloader", err)

	err = preloader.AddImage(ctx, testnet.DockerImageName(version))
	testnet.NoError("failed to add image", err)
	defer func() { _ = preloader.EmptyImages(ctx) }()

	logger.Println("Creating genesis nodes")
	for i := 0; i < numNodes; i++ {
		err := testNet.CreateGenesisNode(ctx, version, 10000000, 0, testnet.DefaultResources, true)
		testnet.NoError("failed to create genesis node", err)
	}

	logger.Println("Creating txsim")
	endpoints, err := testNet.RemoteGRPCEndpoints(ctx)
	testnet.NoError("failed to get remote gRPC endpoints", err)
	upgradeSchedule := map[int64]uint64{
		upgradeHeightV3: v3.Version,
	}

	err = testNet.CreateTxClient(ctx, "txsim", version, 1, "100-2000", 100, testnet.DefaultResources, endpoints[0], upgradeSchedule)
	testnet.NoError("failed to create tx client", err)

	logger.Println("Setting up testnet")
	testnet.NoError("Failed to setup testnet", testNet.Setup(ctx))
	logger.Println("Starting testnet")
	testnet.NoError("Failed to start testnet", testNet.Start(ctx))

	timer := time.NewTimer(10 * time.Minute)
	defer timer.Stop()
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	// checking initial timeouts
	logger.Println("checking initial timeouts")
	for _, node := range testNet.Nodes() {
		client, err := node.Client()
		tInfo, err := client.ConsensusTimeoutsInfo(ctx, 1)
		testnet.NoError("failed to get consensus timeouts info", err)
		logger.Printf("timeout commit: %v, timeout propose: %v", tInfo.TimeoutCommit, tInfo.TimeoutPropose)
		if appconsts.GetTimeoutCommit(v2.Version) != tInfo.TimeoutCommit {
			return fmt.Errorf("timeout commit mismatch at height 1: got %v, expected %v",
				tInfo.TimeoutCommit, appconsts.GetTimeoutCommit(v2.Version))
		}
		if appconsts.GetTimeoutPropose(v2.Version) != tInfo.TimeoutPropose {
			return fmt.Errorf("timeout propose mismatch at height 1: got %v, "+
				"expected %v",
				tInfo.TimeoutPropose, appconsts.GetTimeoutPropose(v2.Version))
		}
	}
	logger.Println("waiting for upgrade")

	// wait for the upgrade to complete
	var upgradedHeight int64
	for _, node := range testNet.Nodes() {
		client, err := node.Client()
		testnet.NoError("failed to get client", err)
		upgradeComplete := false
		lastHeight := int64(0)
		for !upgradeComplete {
			select {
			case <-timer.C:
				return fmt.Errorf("failed to upgrade to v3, last height: %d", lastHeight)
			case <-ticker.C:
				resp, err := client.Header(ctx, nil)
				testnet.NoError("failed to get header", err)
				if resp.Header.Version.App == v3.Version {
					upgradeComplete = true
					if upgradedHeight == 0 {
						upgradedHeight = resp.Header.Height
					}
				}
				logger.Printf("height %v", resp.Header.Height)
				lastHeight = resp.Header.Height
				tInfo, err := client.ConsensusTimeoutsInfo(ctx, lastHeight+1)
				testnet.NoError("failed to get consensus timeouts info", err)
				logger.Printf("timeout commit: %v, timeout propose: %v", tInfo.TimeoutCommit, tInfo.TimeoutPropose)
			}
		}

		logger.Println("upgrade is completed")
		zlog.Info().Str("name", node.Name).Msg("upgrade is completed")

	}

	// now check if the timeouts are set correctly
	zlog.Info().Int("upgradedHeight", int(upgradedHeight)).Msg("checking timeouts")
	for _, node := range testNet.Nodes() {
		zlog.Info().Str("name", node.Name).Msg("checking timeouts")
		for h := int64(1); h <= upgradedHeight+4; h++ {
			client, err := node.Client()
			block, err := client.Block(ctx, &h)
			testnet.NoError("failed to get header", err)

			// timeouts of the next height are set based on the block at the current height
			// so, we retrieve the timeouts of the next height to see if they
			// are set according to the block at the current height
			tInfo, err := client.ConsensusTimeoutsInfo(ctx, h+1)
			testnet.NoError("failed to get consensus timeouts info", err)

			if appconsts.GetTimeoutCommit(block.Block.Header.Version.App) != tInfo.TimeoutCommit {
				return fmt.Errorf("timeout commit mismatch at height %d: got %v, expected %v",
					block.Block.Header.Height, tInfo.TimeoutCommit, appconsts.GetTimeoutCommit(block.Block.Header.Version.App))
			}
			if appconsts.GetTimeoutPropose(block.Block.Header.Version.App) != tInfo.TimeoutPropose {
				return fmt.Errorf("timeout propose mismatch at height %d: got"+
					" %v, expected %v",
					block.Block.Header.Height, tInfo.TimeoutPropose, appconsts.GetTimeoutPropose(block.Block.Header.Version.App))
			}
		}
		zlog.Info().Str("name", node.Name).Msg("timeouts are checked")
	}

	return nil
}
