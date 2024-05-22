package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/celestiaorg/celestia-app/v2/app"
	"github.com/celestiaorg/celestia-app/v2/app/encoding"
	v1 "github.com/celestiaorg/celestia-app/v2/pkg/appconsts/v1"
	v2 "github.com/celestiaorg/celestia-app/v2/pkg/appconsts/v2"
	"github.com/celestiaorg/celestia-app/v2/test/e2e/testnet"
	"github.com/celestiaorg/celestia-app/v2/test/txsim"
	"github.com/celestiaorg/knuu/pkg/knuu"
	"github.com/tendermint/tendermint/rpc/client/http"
)

func MajorUpgradeToV2(logger *log.Logger) error {
	latestVersion, err := testnet.GetLatestVersion()
	testnet.NoError("failed to get latest version", err)

	logger.Println("Running major upgrade to v2 test", "version", latestVersion)

	numNodes := 4
	upgradeHeight := int64(12)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger.Println("Creating testnet")
	testNet, err := testnet.New("runMajorUpgradeToV2", seed, nil, "test")
	testnet.NoError("failed to create testnet", err)

	defer testNet.Cleanup()

	testNet.SetConsensusParams(app.DefaultInitialConsensusParams())

	preloader, err := knuu.NewPreloader()
	testnet.NoError("failed to create preloader", err)

	defer func() { _ = preloader.EmptyImages() }()
	testnet.NoError("failed to add image", preloader.AddImage(testnet.DockerImageName(latestVersion)))

	logger.Println("Creating genesis nodes")
	for i := 0; i < numNodes; i++ {
		err := testNet.CreateGenesisNode(latestVersion, 10000000, upgradeHeight, testnet.DefaultResources)
		testnet.NoError("failed to create genesis node", err)
	}

	kr, err := testNet.CreateAccount("alice", 1e12, "")
	testnet.NoError("failed to create account", err)
	// start the testnet

	logger.Println("Setting up testnet")
	testnet.NoError("Failed to setup testnet", testNet.Setup())
	logger.Println("Starting testnet")
	testnet.NoError("Failed to start testnet", testNet.Start())

	errCh := make(chan error)
	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	opts := txsim.DefaultOptions().WithSeed(seed).SuppressLogs()
	sequences := txsim.NewBlobSequence(txsim.NewRange(200, 4000), txsim.NewRange(1, 3)).Clone(5)
	sequences = append(sequences, txsim.NewSendSequence(4, 1000, 100).Clone(5)...)
	go func() {
		errCh <- txsim.Run(ctx, testNet.GRPCEndpoints()[0], kr, encCfg, opts, sequences...)
	}()

	heightBefore := upgradeHeight - 1
	for i := 0; i < numNodes; i++ {
		client, err := testNet.Node(i).Client()
		testnet.NoError("failed to get client", err)

		testnet.NoError("failed to wait for height", waitForHeight(testNet, testNet.Node(i), upgradeHeight, time.Minute))

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

	// end txsim
	cancel()

	err = <-errCh
	if !strings.Contains(err.Error(), context.Canceled.Error()) {
		return fmt.Errorf("expected context.Canceled error, got: %w", err)
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

func waitForHeight(testnet *testnet.Testnet, node *testnet.Node, height int64, period time.Duration) error {
	timer := time.NewTimer(period)
	ticker := time.NewTicker(100 * time.Millisecond)
	for {
		select {
		case <-timer.C:
			return fmt.Errorf("failed to reach height %d in %.2f seconds", height, period.Seconds())
		case <-ticker.C:
			executor, err := testnet.GetExecutor()
			if err != nil {
				return fmt.Errorf("failed to get executor: %w", err)
			}
			currentHeight, err := node.GetHeight(executor)
			if err != nil {
				return err
			}
			if currentHeight >= height {
				return nil
			}
		}
	}
}
