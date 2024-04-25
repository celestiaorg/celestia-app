package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/celestiaorg/celestia-app/v2/app"
	"github.com/celestiaorg/celestia-app/v2/app/encoding"
	"github.com/celestiaorg/celestia-app/v2/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v2/test/e2e/testnets"
	"github.com/celestiaorg/celestia-app/v2/test/txsim"
	"github.com/celestiaorg/celestia-app/v2/test/util/testnode"
)

// This test runs a simple testnet with 4 validators. It submits both MsgPayForBlobs
// and MsgSends over 30 seconds and then asserts that at least 10 transactions were
// committed.
func E2ESimple(logger *log.Logger) error {
	latestVersion, err := testnets.GetLatestVersion()
	testnets.NoError("failed to get latest version", err)

	logger.Println("Running simple e2e test", "version", latestVersion)

	testnet, err := testnets.New("E2ESimple", seed, nil)
	testnets.NoError("failed to create testnets", err)
	defer testnet.Cleanup()

	logger.Println("Creating testnet validators")
	testnets.NoError("failed to create genesis nodes", testnet.CreateGenesisNodes(4, latestVersion, 10000000, 0, testnets.DefaultResources))

	logger.Println("Creating account")
	kr, err := testnet.CreateAccount("alice", 1e12, "")
	testnets.NoError("failed to create account", err)

	logger.Println("Setting up testnets")
	testnets.NoError("failed to setup testnets", testnet.Setup())

	logger.Println("Starting testnets")
	testnets.NoError("failed to start testnets", testnet.Start())

	logger.Println("Running txsim")
	sequences := txsim.NewBlobSequence(txsim.NewRange(200, 4000), txsim.NewRange(1, 3)).Clone(5)
	sequences = append(sequences, txsim.NewSendSequence(4, 1000, 100).Clone(5)...)

	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	opts := txsim.DefaultOptions().WithSeed(seed).SuppressLogs()
	err = txsim.Run(ctx, testnet.GRPCEndpoints()[0], kr, encCfg, opts, sequences...)

	if !errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("expected context.DeadlineExceeded, got %w", err)
	}

	logger.Println("Reading blockchain")
	blockchain, err := testnode.ReadBlockchain(context.Background(), testnet.Node(0).AddressRPC())
	testnets.NoError("failed to read blockchain", err)

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
