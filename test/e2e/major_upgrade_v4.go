package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/celestiaorg/celestia-app/v4/test/util/genesis"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"log"
	"time"

	"github.com/celestiaorg/knuu/pkg/knuu"

	"github.com/celestiaorg/celestia-app/v4/app"
	"github.com/celestiaorg/celestia-app/v4/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v4/test/e2e/testnet"
)

func MajorUpgradeToV4(logger *log.Logger) error {
	testName := "MajorUpgradeToV4"
	numNodes := 4
	//upgradeHeightV3 := int64(15)
	upgradeHeightV4 := int64(30)

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

	convertV4ToV3Genesis := func(state map[string]json.RawMessage) map[string]json.RawMessage {
		state[banktypes.ModuleName] = []byte(`{"send_enabled":true}`)
		banktypes.GenesisState{}
		return state
	}

	logger.Println("Creating testnet")
	testNet, err := testnet.New(logger, kn, testnet.Options{
		ChainID: appconsts.TestChainID,
		GenesisModifiers: []genesis.Modifier{
			convertV4ToV3Genesis,
		},
	})
	testnet.NoError("failed to create testnet", err)

	defer testNet.Cleanup(ctx)

	latestVersion, err := testnet.GetLatestVersion()
	testnet.NoError("failed to get latest version", err)
	latestVersion = "pr-82"

	consensusParams := app.DefaultConsensusParams()
	consensusParams.Version.App = 3 // Start the test on v3
	testNet.SetConsensusParams(consensusParams)

	preloader, err := testNet.NewPreloader()
	testnet.NoError("failed to create preloader", err)

	err = preloader.AddImage(ctx, "ghcr.io/01builders/celestia-app-multiplexer:"+latestVersion)
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
		//upgradeHeightV3: 3,
		upgradeHeightV4: 4,
	}

	err = testNet.CreateTxClient(ctx, "txsim", "latest", 1, "100-2000", 100, testnet.DefaultResources, endpoints[0], upgradeSchedule)
	testnet.NoError("failed to create tx client", err)

	logger.Println("Setting up testnet")

	testnet.NoError("Failed to setup testnet", testNet.Setup(ctx, testnet.WithPrometheus(false)))
	logger.Println("Starting testnet")
	testnet.NoError("Failed to start testnet", testNet.Start(ctx))

	timer := time.NewTimer(20 * time.Minute)
	defer timer.Stop()
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	logger.Println("waiting for upgrade")

	// wait for the upgrade to complete
	//var upgradedHeightV3 int64
	//for _, node := range testNet.Nodes() {
	//	client, err := node.Client()
	//	testnet.NoError("failed to get client", err)
	//	upgradeComplete := false
	//	lastHeight := int64(0)
	//	for !upgradeComplete {
	//		select {
	//		case <-timer.C:
	//			return fmt.Errorf("failed to upgrade to v3, last height: %d", lastHeight)
	//		case <-ticker.C:
	//			resp, err := client.Header(ctx, nil)
	//			testnet.NoError("failed to get header", err)
	//			if resp.Header.Version.App == 3 {
	//				upgradeComplete = true
	//				if upgradedHeightV3 == 0 {
	//					upgradedHeightV3 = resp.Header.Height
	//				}
	//			}
	//			logger.Printf("height %v", resp.Header.Height)
	//			lastHeight = resp.Header.Height
	//		}
	//	}
	//}

	// wait for the upgrade to complete
	var upgradedHeightV4 int64
	for _, node := range testNet.Nodes() {
		client, err := node.Client()
		testnet.NoError("failed to get client", err)
		upgradeComplete := false
		lastHeight := int64(0)
		for !upgradeComplete {
			select {
			case <-timer.C:
				return fmt.Errorf("failed to upgrade to v4, last height: %d", lastHeight)
			case <-ticker.C:
				resp, err := client.Header(ctx, nil)
				testnet.NoError("failed to get header", err)
				if resp.Header.Version.App == 4 {
					upgradeComplete = true
					if upgradedHeightV4 == 0 {
						upgradedHeightV4 = resp.Header.Height
					}
				}
				logger.Printf("height %v", resp.Header.Height)
				lastHeight = resp.Header.Height
			}
		}
	}

	logger.Printf("upgraded height: %v", upgradedHeightV4)

	return nil
}
