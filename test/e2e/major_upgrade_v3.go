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
	"github.com/celestiaorg/knuu/pkg/knuu"
	tmtypes "github.com/tendermint/tendermint/types"
)

func MajorUpgradeToV3(logger *log.Logger) error {
	testName := "MajorUpgradeToV3"
	numNodes := 4
	upgradeHeightV3 := int64(15)

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
	testNet, err := testnet.New(logger, kn, testnet.Options{
		ChainID: appconsts.TestChainID,
	})
	testnet.NoError("failed to create testnet", err)

	defer testNet.Cleanup(ctx)

	latestVersion, err := testnet.GetLatestVersion()
	testnet.NoError("failed to get latest version", err)

	consensusParams := app.DefaultConsensusParams()
	consensusParams.Version.AppVersion = v2.Version // Start the test on v2
	testNet.SetConsensusParams(consensusParams)

	preloader, err := testNet.NewPreloader()
	testnet.NoError("failed to create preloader", err)

	err = preloader.AddImage(ctx, testnet.DockerImageName(latestVersion))
	testnet.NoError("failed to add image", err)
	defer func() { _ = preloader.EmptyImages(ctx) }()

	logger.Println("Creating genesis nodes")
	for i := 0; i < numNodes; i++ {
		err := testNet.CreateGenesisNode(ctx, latestVersion, 10000000, 0, testnet.DefaultResources, true)
		testnet.NoError("failed to create genesis node", err)
	}

	logger.Println("Creating txsim")
	endpoints, err := testNet.RemoteGRPCEndpoints()
	testnet.NoError("failed to get remote gRPC endpoints", err)
	upgradeSchedule := map[int64]uint64{
		upgradeHeightV3: v3.Version,
	}

	err = testNet.CreateTxClient(ctx, "txsim", latestVersion, 1, "100-2000", 100, testnet.DefaultResources, endpoints[0], upgradeSchedule)
	testnet.NoError("failed to create tx client", err)

	logger.Println("Setting up testnet")
	testnet.NoError("Failed to setup testnet", testNet.Setup(ctx))
	logger.Println("Starting testnet")
	testnet.NoError("Failed to start testnet", testNet.Start(ctx))

	timer := time.NewTimer(20 * time.Minute)
	defer timer.Stop()
	ticker := time.NewTicker(3 * time.Second)
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

	logger.Printf("upgraded height: %v", upgradedHeight)

	// check if the timeouts are set correctly
	rpcNode := testNet.Nodes()[0]
	client, err := rpcNode.Client()
	testnet.NoError("failed to get client", err)

	startHeight := upgradeHeightV3 - 5
	endHeight := upgradedHeight + 5

	type versionDuration struct {
		dur   time.Duration
		block *tmtypes.Block
	}

	// wait until endHeight is reached
	testnet.NoError("failed to wait for height", waitForHeight(ctx, client, endHeight, 5*time.Minute))

	blockSummaries := make([]versionDuration, 0, endHeight-startHeight)
	var prevBlockTime time.Time

	for h := startHeight; h < endHeight; h++ {
		resp, err := client.Block(ctx, &h)
		testnet.NoError("failed to get header", err)
		blockTime := resp.Block.Time

		if h == startHeight {
			if resp.Block.Version.App != v2.Version {
				return fmt.Errorf("expected start height %v was app version 2", startHeight)
			}
			prevBlockTime = blockTime
			continue
		}

		blockDur := blockTime.Sub(prevBlockTime)
		prevBlockTime = blockTime
		blockSummaries = append(blockSummaries, versionDuration{dur: blockDur, block: resp.Block})
	}

	preciseUpgradeHeight := 0
	multipleRounds := 0
	for _, b := range blockSummaries {

		// check for the precise upgrade height and skip, as the block time
		// won't match due to the off by 1 nature of the block time.
		if b.block.Version.App == v3.Version && preciseUpgradeHeight == 0 {
			preciseUpgradeHeight = int(b.block.Height)
			continue
		}

		// don't test heights with multiple rounds as the times are off and fail
		// later if there are too many
		if b.block.LastCommit.Round > 0 {
			multipleRounds++
			continue
		}

		if b.dur < appconsts.GetTimeoutCommit(b.block.Version.App) {
			return fmt.Errorf(
				"block was too fast for corresponding version: version %v duration %v upgrade height %v height %v",
				b.block.Version.App,
				b.dur,
				preciseUpgradeHeight,
				b.block.Height,
			)
		}

		// check if the time decreased for v3
		if b.block.Version.App == v3.Version && b.dur > appconsts.GetTimeoutCommit(b.block.Version.App)+5 {
			return fmt.Errorf(
				"block was too slow for corresponding version: version %v duration %v upgrade height %v height %v",
				b.block.Version.App,
				b.dur,
				preciseUpgradeHeight,
				b.block.Height,
			)
		}

	}

	if multipleRounds > 2 {
		return fmt.Errorf("too many multiple rounds for test to be reliable: %d", multipleRounds)
	}

	return nil
}
