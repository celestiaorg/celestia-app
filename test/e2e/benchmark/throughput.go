package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/celestiaorg/celestia-app/v2/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v2/test/e2e/testnet"
	"github.com/celestiaorg/celestia-app/v2/test/util/testnode"
	"github.com/tendermint/tendermint/pkg/trace"
)

const (
	seed = 42

	toMiB = 1024 * 1024
	toMB  = 1000 * 1000
)

var bigBlockManifest = Manifest{
	ChainID:    "test",
	Validators: 2,
	TxClients:  2,
	ValidatorResource: testnet.Resources{
		MemoryRequest: "12Gi",
		MemoryLimit:   "12Gi",
		CPU:           "8",
		Volume:        "20Gi",
	},
	TxClientsResource: testnet.Resources{
		MemoryRequest: "1Gi",
		MemoryLimit:   "3Gi",
		CPU:           "2",
		Volume:        "1Gi",
	},
	SelfDelegation:     10000000,
	CelestiaAppVersion: "pr-3261",
	TxClientVersion:    "pr-3261",
	EnableLatency:      false,
	LatencyParams:      LatencyParams{0, 0}, // in  milliseconds
	BlobSequences:      60,
	BlobsPerSeq:        6,
	BlobSizes:          "200000",
	PerPeerBandwidth:   5 * toMB,
	UpgradeHeight:      0,
	TimeoutCommit:      11 * time.Second,
	TimeoutPropose:     80 * time.Second,
	Mempool:            "v1", // ineffective as it always defaults to v1
	BroadcastTxs:       true,
	Prometheus:         false,
	GovMaxSquareSize:   512,
	MaxBlockBytes:      7800000,
	TestDuration:       15 * time.Minute,
	LocalTracingType:   "local",
	PushTrace:          true,
}

func TwoNodeSimple(logger *log.Logger) error {
	latestVersion, err := testnet.GetLatestVersion()
	testnet.NoError("failed to get latest version", err)

	logger.Println("=== RUN TwoNodeSimple", "version:", latestVersion)

	manifest := Manifest{
		ChainID:            "test-e2e-two-node-simple",
		Validators:         2,
		ValidatorResource:  testnet.DefaultResources,
		TxClientsResource:  testnet.DefaultResources,
		SelfDelegation:     10000000,
		CelestiaAppVersion: latestVersion,
		TxClientVersion:    testnet.TxsimVersion,
		EnableLatency:      true,
		LatencyParams:      LatencyParams{100, 10}, // in  milliseconds
		BlobsPerSeq:        1,
		BlobSequences:      1,
		BlobSizes:          "10000-10000",
		PerPeerBandwidth:   5 * 1024 * 1024,
		UpgradeHeight:      0,
		TimeoutCommit:      1 * time.Second,
		TimeoutPropose:     1 * time.Second,
		Mempool:            "v1",
		BroadcastTxs:       true,
		Prometheus:         true,
		GovMaxSquareSize:   appconsts.DefaultGovMaxSquareSize,
		MaxBlockBytes:      appconsts.DefaultMaxBytes,
		LocalTracingType:   "local",
		PushTrace:          false,
		DownloadTraces:     false,
		TestDuration:       30 * time.Second,
		TxClients:          2,
	}

	benchTest, err := NewBenchmarkTest("E2EThroughput", &manifest)
	testnet.NoError("failed to create benchmark test", err)

	defer func() {
		log.Print("Cleaning up testnet")
		benchTest.Cleanup()
	}()

	testnet.NoError("failed to setup nodes", benchTest.SetupNodes())

	testnet.NoError("failed to run the benchmark test", benchTest.Run())

	// post test data collection and validation

	// if local tracing is enabled,
	// pull round state traces to confirm tracing is working as expected.
	if benchTest.manifest.LocalTracingType == "local" {
		if _, err := benchTest.Node(0).PullRoundStateTraces("."); err != nil {
			return fmt.Errorf("failed to pull round state traces: %w", err)
		}
	}

	// download traces from S3, if enabled
	if benchTest.manifest.PushTrace && benchTest.manifest.DownloadTraces {
		// download traces from S3
		pushConfig, _ := trace.GetPushConfigFromEnv()
		err := trace.S3Download("./traces/", benchTest.manifest.ChainID,
			pushConfig)
		if err != nil {
			return fmt.Errorf("failed to download traces from S3: %w", err)
		}
	}

	log.Println("Reading blockchain")
	blockchain, err := testnode.ReadBlockchain(context.Background(),
		benchTest.Node(0).AddressRPC())
	testnet.NoError("failed to read blockchain", err)

	totalTxs := 0
	for _, block := range blockchain {
		if appconsts.LatestVersion != block.Version.App {
			return fmt.Errorf("expected app version %d, got %d", appconsts.LatestVersion, block.Version.App)
		}
		totalTxs += len(block.Data.Txs)
	}
	if totalTxs < 10 {
		return fmt.Errorf("expected at least 10 transactions, got %d", totalTxs)
	}

	return nil
}

func TwoNodeBigBlock_8MiB(logger *log.Logger) error {
	logger.Println("Running TwoNodeBigBlock_8MiB")
	manifest := bigBlockManifest
	manifest.TestnetName = "TwoNodeBigBlock_8MiB"
	manifest.MaxBlockBytes = 8 * toMB
	manifest.ChainID = "two-node-big-block-8mb"
	logger.Println("ChainID: ", manifest.ChainID)

	benchTest, err := NewBenchmarkTest("TwoNodeBigBlock_8MiB", &manifest)
	testnet.NoError("failed to create benchmark test", err)

	defer func() {
		log.Print("Cleaning up testnet")
		benchTest.Cleanup()
	}()

	testnet.NoError("failed to setup nodes", benchTest.SetupNodes())
	testnet.NoError("failed to run the benchmark test", benchTest.Run())
	testnet.NoError("failed to check results", benchTest.CheckResults())

	log.Println("--- PASS ✅: TwoNodeBigBlock_8MiB")
	return nil
}

func TwoNodeBigBlock_8MiB_Latency(logger *log.Logger) error {
	logger.Println("Running TwoNodeBigBlock_8MiB_Latency")
	manifest := bigBlockManifest
	manifest.TestnetName = "TwoNodeBigBlock_8MiB_Latency"
	manifest.ChainID = "two-node-big-block-8mib-latency"
	manifest.MaxBlockBytes = 8 * toMiB
	manifest.EnableLatency = true

	benchTest, err := NewBenchmarkTest("TwoNodeBigBlock_8MiB_Latency", &manifest)
	testnet.NoError("failed to create benchmark test", err)

	defer func() {
		log.Print("Cleaning up testnet")
		benchTest.Cleanup()
	}()

	testnet.NoError("failed to setup nodes", benchTest.SetupNodes())
	testnet.NoError("failed to run the benchmark test", benchTest.Run())
	testnet.NoError("failed to check results", benchTest.CheckResults())

	log.Println("--- PASS ✅: TwoNodeBigBlock_8MiB_Latency")
	return nil
}

func TwoNodeBigBlock_32MiB(logger *log.Logger) error {
	logger.Println("Running TwoNodeBigBlock_32MiB")
	manifest := bigBlockManifest
	manifest.TestnetName = "TwoNodeBigBlock_32MiB"
	manifest.ChainID = "two-node-big-block-32mb"
	manifest.MaxBlockBytes = 32 * toMB

	benchTest, err := NewBenchmarkTest("TwoNodeBigBlock_32MiB", &manifest)
	testnet.NoError("failed to create benchmark test", err)

	defer func() {
		log.Print("Cleaning up testnet")
		benchTest.Cleanup()
	}()

	testnet.NoError("failed to setup nodes", benchTest.SetupNodes())
	testnet.NoError("failed to run the benchmark test", benchTest.Run())
	testnet.NoError("failed to check results", benchTest.CheckResults())

	log.Println("--- PASS ✅: TwoNodeBigBlock_32MiB")
	return nil
}

func TwoNodeBigBlock_64MiB(logger *log.Logger) error {
	logger.Println("Running TwoNodeBigBlock_64MiB")
	manifest := bigBlockManifest
	manifest.ChainID = "two-node-big-block-64mb"
	manifest.MaxBlockBytes = 64 * toMB

	benchTest, err := NewBenchmarkTest("TwoNodeBigBlock_64MiB", &manifest)
	testnet.NoError("failed to create benchmark test", err)

	defer func() {
		log.Print("Cleaning up testnet")
		benchTest.Cleanup()
	}()

	testnet.NoError("failed to setup nodes", benchTest.SetupNodes())
	testnet.NoError("failed to run the benchmark test", benchTest.Run())
	testnet.NoError("failed to check results", benchTest.CheckResults())

	log.Println("--- PASS ✅: TwoNodeBigBlock_64MiB")
	return nil
}
