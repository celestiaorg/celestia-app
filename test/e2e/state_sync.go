package main

import (
	"context"
	"fmt"
	"log"
	"time"

	v2 "github.com/celestiaorg/celestia-app/v3/pkg/appconsts/v2"
	"github.com/celestiaorg/celestia-app/v3/test/e2e/testnet"
	"github.com/tendermint/tendermint/crypto/ed25519"
	"github.com/tendermint/tendermint/rpc/client/http"
	"github.com/tendermint/tendermint/types"
)

type stateSyncTest struct {
	testNet     *testnet.Testnet
	validator   *testnet.Node
	syncingNode *testnet.Node
}

func runStateSyncTest(ctx context.Context, appVersion uint64, logger *log.Logger) {
	testName := fmt.Sprintf("StateSync-v%v", appVersion)

	testNet, err := baseTestnet(ctx, logger, testName, 3, appVersion, map[int64]uint64{})
	testnet.NoError("failed to create testnet", err)

	defer testNet.Cleanup(ctx)

	latestVersion, err := testnet.GetLatestVersion()
	testnet.NoError("failed to get latest version", err)

	// create the node that will stateSync
	skey, nkey := ed25519.GenPrivKey(), ed25519.GenPrivKey()
	node, err := testnet.NewNode(
		ctx,
		"sync node",
		latestVersion,
		1,
		0,
		nil,
		skey,
		nkey,
		0,
		testnet.DefaultResources,
		testNet.Grafana(),
		testNet.Knuu(),
		true,
	)
	testnet.NoError("failure to create state sync node", err)

	sst := &stateSyncTest{
		testNet:     testNet,
		validator:   testNet.Nodes()[0],
		syncingNode: node,
	}

	defer testNet.Cleanup(ctx)

	testnet.NoError("failed to setup tesnet", testNet.Setup(ctx))
	testnet.NoError("failed to start the network", testNet.Start(ctx))

	client, err := sst.validator.Client()
	testnet.NoError("failure to get client", err)

	block, err := waitUntil(ctx, client, 20, logger)
	testnet.NoError("failure to wait until", err)

	trustedHash := block.Header.Hash()
	trustedHeight := block.Height
	valRPC := sst.validator.AddressRPC()
	valP2P := sst.validator.AddressP2P(ctx, true)

	logger.Println(trustedHash, trustedHeight, valRPC, valP2P)

	return
}

func StateSync(logger *log.Logger) error {
	ctx := context.TODO()
	runStateSyncTest(ctx, v2.Version, logger)
	return nil
}

func waitUntil(ctx context.Context, client *http.HTTP, height int64, logger *log.Logger) (*types.Block, error) {
	timer := time.NewTimer(20 * time.Minute)
	defer timer.Stop()
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	lastHeight := 0
	for {
		select {
		case <-timer.C:
			return nil, fmt.Errorf("failed to upgrade to v3, last height: %d", lastHeight)
		case <-ticker.C:
			resp, err := client.Block(ctx, nil)
			testnet.NoError("failed to get header", err)
			if resp.Block.Height >= height {
				return resp.Block, nil
			}
			lastHeight = int(resp.Block.Height)
			logger.Println("Waiting, last height", lastHeight)

		}
	}
}
