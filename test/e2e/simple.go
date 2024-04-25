package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/celestiaorg/celestia-app/v2/app"
	"github.com/celestiaorg/celestia-app/v2/app/encoding"
	"github.com/celestiaorg/celestia-app/v2/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v2/test/e2e/testnet"
	"github.com/celestiaorg/celestia-app/v2/test/txsim"
	"github.com/celestiaorg/celestia-app/v2/test/util/testnode"
)

// This test runs a simple testnet with 4 validators. It submits both MsgPayForBlobs
// and MsgSends over 30 seconds and then asserts that at least 10 transactions were
// committed.
func E2ESimple(logger *log.Logger) error {
	os.Setenv("KNUU_NAMESPACE", "test")

	latestVersion, err := testnet.GetLatestVersion()
	testnet.NoError("failed to get latest version", err)

	logger.Println("Running simple e2e test", "version", latestVersion)

	testNet, err := testnet.New("E2ESimple", seed, nil)
	testnet.NoError("failed to create testnet", err)

	defer testNet.Cleanup()

	logger.Println("Creating testnet validators")
	testnet.NoError("failed to create genesis nodes", testNet.CreateGenesisNodes(4, latestVersion, 10000000, 0, testnet.DefaultResources))

	logger.Println("Creating account")
	kr, err := testNet.CreateAccount("alice", 1e12, "")
	testnet.NoError("failed to create account", err)

	logger.Println("Setting up testnets")
	testnet.NoError("failed to setup testnets", testNet.Setup())

	logger.Println("Starting testnets")
	testnet.NoError("failed to start testnets", testNet.Start())

	logger.Println("Running txsim")
	sequences := txsim.NewBlobSequence(txsim.NewRange(200, 4000), txsim.NewRange(1, 3)).Clone(5)
	sequences = append(sequences, txsim.NewSendSequence(4, 1000, 100).Clone(5)...)

	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	opts := txsim.DefaultOptions().WithSeed(seed).SuppressLogs()
	err = txsim.Run(ctx, testNet.GRPCEndpoints()[0], kr, encCfg, opts, sequences...)

	if !errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("expected context.DeadlineExceeded, got %w", err)
	}

	logger.Println("Reading blockchain")
	blockchain, err := testnode.ReadBlockchain(context.Background(), testNet.Node(0).AddressRPC())
	testnet.NoError("failed to read blockchain", err)

	totalTxs := 0
	for _, block := range blockchain {
		if appconsts.LatestVersion != block.Version.App {
			return fmt.Errorf("expected app version %d, got %d in block %d", appconsts.LatestVersion, block.Version.App, block.Height)
		}
		totalTxs += len(block.Data.Txs)
	}
	if totalTxs < 10 {
		return fmt.Errorf("expected at least 10 transactions, got %d", totalTxs)
	}
	return nil
}
