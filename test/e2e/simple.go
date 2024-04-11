package main

import (
	"context"
	"errors"
	"log"
	"os"
	"time"

	"github.com/celestiaorg/celestia-app/v2/app"
	"github.com/celestiaorg/celestia-app/v2/app/encoding"
	"github.com/celestiaorg/celestia-app/v2/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v2/test/e2e/pkg"
	"github.com/celestiaorg/celestia-app/v2/test/txsim"
	"github.com/celestiaorg/celestia-app/v2/test/util/testnode"
)

// This test runs a simple testnet with 4 validators. It submits both MsgPayForBlobs
// and MsgSends over 30 seconds and then asserts that at least 10 transactions were
// committed.
func E2ESimple(logger *log.Logger) error {
	if os.Getenv("KNUU_NAMESPACE") != "test" {
		logger.Fatal("skipping e2e test")
	}

	if os.Getenv("E2E_LATEST_VERSION") != "" {
		latestVersion = os.Getenv("E2E_LATEST_VERSION")
		_, isSemVer := pkg.ParseVersion(latestVersion)
		switch {
		case isSemVer:
		case latestVersion == "latest":
		case len(latestVersion) == 7:
		case len(latestVersion) >= 8:
			// assume this is a git commit hash (we need to trim the last digit to match the docker image tag)
			latestVersion = latestVersion[:7]
		default:
			logger.Fatalf("unrecognised version: %s", latestVersion)
		}
	}
	logger.Println("Running simple e2e test", "version", latestVersion)

	testnet, err := pkg.New("E2ESimple", seed, pkg.GetGrafanaInfoFromEnvVar())
	NoError("failed to create testnet", err)
	defer testnet.Cleanup()

	logger.Println("Creating testnet validators")
	err = testnet.CreateGenesisNodes(4, latestVersion, 10000000, 0, pkg.DefaultResources)
	NoError("failed to create genesis nodes", err)

	logger.Println("Creating account")
	kr, err := testnet.CreateAccount("alice", 1e12, "")
	NoError("failed to create account", err)

	logger.Println("Setting up testnet")
	err = testnet.Setup()
	NoError("failed to setup testnet", err)

	logger.Println("Starting testnet")
	err = testnet.Start()
	NoError("failed to start testnet", err)

	logger.Println("Running txsim")
	sequences := txsim.NewBlobSequence(txsim.NewRange(200, 4000), txsim.NewRange(1, 3)).Clone(5)
	sequences = append(sequences, txsim.NewSendSequence(4, 1000, 100).Clone(5)...)

	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	opts := txsim.DefaultOptions().WithSeed(seed).SuppressLogs()
	err = txsim.Run(ctx, testnet.GRPCEndpoints()[0], kr, encCfg, opts, sequences...)

	if !errors.Is(err, context.DeadlineExceeded) {
		logger.Fatal("Expected context.DeadlineExceeded, got %v", err)
	}

	logger.Println("Reading blockchain")
	blockchain, err := testnode.ReadBlockchain(context.Background(), testnet.Node(0).AddressRPC())
	NoError("failed to read blockchain", err)

	totalTxs := 0
	for _, block := range blockchain {
		if appconsts.LatestVersion != block.Version.App {
			logger.Fatalf("expected app version %s, got %s", appconsts.LatestVersion, block.Version.App)
		}
		totalTxs += len(block.Data.Txs)
	}
	if totalTxs < 10 {
		logger.Fatalf("expected at least 10 transactions, got %d", totalTxs)
	}
	return nil
}
