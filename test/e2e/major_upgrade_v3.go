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
	"github.com/celestiaorg/knuu/pkg/knuu"
)

func MajorUpgradeToV3(logger *log.Logger) error {
	testName := "MajorUpgradeToV3"
	numNodes := 4
	upgradeHeightV3 := int64(10)

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
	testNet, err := testnet.New(kn, testnet.Options{})
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

	timer := time.NewTimer(20 * time.Minute)
	defer timer.Stop()
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

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
			}
		}
	}

	// check if the timeouts are set correctly
	rpcNode := testNet.Nodes()[0]
	client, err := rpcNode.Client()
	testnet.NoError("failed to get client", err)
	blockTimes := make([]time.Duration, 0, 7)
	var prevBlockTime time.Time
	for h := upgradedHeight - 4; h <= upgradedHeight+4; h++ {
		resp, err := client.Header(ctx, &h)
		testnet.NoError("failed to get header", err)
		blockTime := resp.Header.Time
		if prevBlockTime.IsZero() {
			prevBlockTime = blockTime
			continue
		}

		blockDur := blockTime.Sub(prevBlockTime)
		prevBlockTime = blockTime
		blockTimes = append(blockTimes, blockDur)
	}

	if len(blockTimes) < 7 {
		testnet.NoError("", fmt.Errorf("not enough block times collected: %v", len(blockTimes)))
	}

	startDur := blockTimes[0]
	endDur := blockTimes[len(blockTimes)-1]

	if startDur < time.Second*10 {
		testnet.NoError("", fmt.Errorf("blocks for v2 are too short %v", len(blockTimes)))
	}

	if endDur > time.Second*7 {
		testnet.NoError("", fmt.Errorf("blocks for v3 are too long %v", len(blockTimes)))
	}

	return nil
}
