package main

import (
	"log"
	"time"

	"github.com/celestiaorg/celestia-app/v2/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v2/test/e2e/testnet"
)

const (
	seed = 42

	mib = 1024 * 1024
	mb  = 1000 * 1000
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
	SelfDelegation: 10000000,
	// @TODO Update the CelestiaAppVersion and  TxClientVersion to the latest
	// version of the main branch once the PR#3261 is merged by addressing this
	// issue https://github.com/celestiaorg/celestia-app/issues/3603.
	CelestiaAppVersion: "pr-3261",
	TxClientVersion:    "pr-3261",
	EnableLatency:      false,
	LatencyParams:      LatencyParams{70, 0}, // in  milliseconds
	BlobSequences:      60,
	BlobsPerSeq:        6,
	BlobSizes:          "200000",
	PerPeerBandwidth:   5 * mb,
	UpgradeHeight:      0,
	TimeoutCommit:      11 * time.Second,
	TimeoutPropose:     80 * time.Second,
	Mempool:            "v1", // ineffective as it always defaults to v1
	BroadcastTxs:       true,
	Prometheus:         false,
	GovMaxSquareSize:   512,
	MaxBlockBytes:      7800000,
	TestDuration:       10 * time.Minute,
	LocalTracingType:   "local",
	PushTrace:          true,
}

func TwoNodeSimple(logger *log.Logger) error {
	latestVersion, err := testnet.GetLatestVersion()
	testnet.NoError("failed to get latest version", err)

	testName := "TwoNodeSimple"
	logger.Printf("Running %s\n", testName)
	logger.Println("version", latestVersion)

	manifest := Manifest{
		ChainID:            "test-e2e-two-node-simple",
		Validators:         2,
		ValidatorResource:  testnet.DefaultResources,
		TxClientsResource:  testnet.DefaultResources,
		SelfDelegation:     10000000,
		CelestiaAppVersion: latestVersion,
		TxClientVersion:    latestVersion,
		EnableLatency:      false,
		LatencyParams:      LatencyParams{70, 0}, // in  milliseconds
		BlobsPerSeq:        6,
		BlobSequences:      60,
		BlobSizes:          "200000",
		PerPeerBandwidth:   5 * mb,
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
		TestDuration:       3 * time.Minute,
		TxClients:          2,
	}

	benchTest, err := NewBenchmarkTest(testName, &manifest)
	testnet.NoError("failed to create benchmark test", err)

	defer func() {
		log.Print("Cleaning up testnet")
		benchTest.Cleanup()
	}()

	testnet.NoError("failed to setup nodes", benchTest.SetupNodes())

	testnet.NoError("failed to run the benchmark test", benchTest.Run())

	testnet.NoError("failed to check results", benchTest.CheckResults(1*mb))

	return nil
}

func TwoNodeBigBlock8MB(logger *log.Logger) error {
	testName := "TwoNodeBigBlock8MB"
	logger.Printf("Running %s\n", testName)
	manifest := bigBlockManifest
	manifest.MaxBlockBytes = 8 * mb

	manifest.ChainID = manifest.summary()
	log.Println("ChainID: ", manifest.ChainID)

	benchTest, err := NewBenchmarkTest(testName, &manifest)
	testnet.NoError("failed to create benchmark test", err)

	defer func() {
		log.Print("Cleaning up testnet")
		benchTest.Cleanup()
	}()

	testnet.NoError("failed to setup nodes", benchTest.SetupNodes())
	testnet.NoError("failed to run the benchmark test", benchTest.Run())
	expectedBlockSize := int64(0.90 * float64(manifest.MaxBlockBytes))
	testnet.NoError("failed to check results", benchTest.CheckResults(expectedBlockSize))

	return nil
}

func TwoNodeBigBlock8MBLatency(logger *log.Logger) error {
	testName := "TwoNodeBigBlock8MBLatency"
	logger.Printf("Running %s\n", testName)
	manifest := bigBlockManifest

	manifest.MaxBlockBytes = 8 * mb
	manifest.EnableLatency = true

	manifest.ChainID = manifest.summary()
	log.Println("ChainID: ", manifest.ChainID)
	benchTest, err := NewBenchmarkTest(testName, &manifest)
	testnet.NoError("failed to create benchmark test", err)

	defer func() {
		log.Print("Cleaning up testnet")
		benchTest.Cleanup()
	}()

	testnet.NoError("failed to setup nodes", benchTest.SetupNodes())
	testnet.NoError("failed to run the benchmark test", benchTest.Run())
	expectedBlockSize := int64(0.90 * float64(manifest.MaxBlockBytes))
	testnet.NoError("failed to check results", benchTest.CheckResults(expectedBlockSize))

	return nil
}

func TwoNodeBigBlock32MB(logger *log.Logger) error {
	testName := "TwoNodeBigBlock32MB"
	logger.Printf("Running %s\n", testName)
	manifest := bigBlockManifest
	manifest.MaxBlockBytes = 32 * mb

	manifest.ChainID = manifest.summary()
	log.Println("ChainID: ", manifest.ChainID)
	benchTest, err := NewBenchmarkTest(testName, &manifest)
	testnet.NoError("failed to create benchmark test", err)

	defer func() {
		log.Print("Cleaning up testnet")
		benchTest.Cleanup()
	}()

	testnet.NoError("failed to setup nodes", benchTest.SetupNodes())
	testnet.NoError("failed to run the benchmark test", benchTest.Run())
	expectedBlockSize := int64(0.90 * float64(manifest.MaxBlockBytes))
	testnet.NoError("failed to check results", benchTest.CheckResults(expectedBlockSize))

	return nil
}

func TwoNodeBigBlock64MB(logger *log.Logger) error {
	testName := "TwoNodeBigBlock64MB"
	logger.Printf("Running %s\n", testName)

	manifest := bigBlockManifest
	manifest.MaxBlockBytes = 64 * mb

	manifest.ChainID = manifest.summary()
	log.Println("ChainID: ", manifest.ChainID)
	benchTest, err := NewBenchmarkTest(testName, &manifest)
	testnet.NoError("failed to create benchmark test", err)

	defer func() {
		log.Print("Cleaning up testnet")
		benchTest.Cleanup()
	}()

	testnet.NoError("failed to setup nodes", benchTest.SetupNodes())
	testnet.NoError("failed to run the benchmark test", benchTest.Run())
	expectedBlockSize := int64(0.90 * float64(manifest.MaxBlockBytes))
	testnet.NoError("failed to check results", benchTest.CheckResults(expectedBlockSize))

	return nil
}

func LargeNetworkBigBlock8MB(logger *log.Logger) error {
	testName := "LargeNetworkBigBlock8MB"
	logger.Printf("Running %s\n", testName)

	manifest := bigBlockManifest
	manifest.MaxBlockBytes = 8 * mb
	manifest.Validators = 50
	manifest.TxClients = 50
	manifest.BlobSequences = 2

	manifest.ChainID = manifest.summary()
	log.Println("ChainID: ", manifest.ChainID)
	benchTest, err := NewBenchmarkTest(testName, &manifest)
	testnet.NoError("failed to create benchmark test", err)

	defer func() {
		log.Print("Cleaning up testnet")
		benchTest.Cleanup()
	}()

	testnet.NoError("failed to setup nodes", benchTest.SetupNodes())
	testnet.NoError("failed to run the benchmark test", benchTest.Run())
	expectedBlockSize := int64(0.90 * float64(manifest.MaxBlockBytes))
	testnet.NoError("failed to check results", benchTest.CheckResults(expectedBlockSize))

	return nil
}

func LargeNetworkBigBlock32MB(logger *log.Logger) error {
	testName := "LargeNetworkBigBlock32MB"
	logger.Printf("Running %s\n", testName)

	manifest := bigBlockManifest
	manifest.MaxBlockBytes = 32 * mb
	manifest.Validators = 50
	manifest.TxClients = 50
	manifest.BlobSequences = 2

	manifest.ChainID = manifest.summary()
	log.Println("ChainID: ", manifest.ChainID)
	benchTest, err := NewBenchmarkTest(testName, &manifest)
	testnet.NoError("failed to create benchmark test", err)

	defer func() {
		log.Print("Cleaning up testnet")
		benchTest.Cleanup()
	}()

	testnet.NoError("failed to setup nodes", benchTest.SetupNodes())
	testnet.NoError("failed to run the benchmark test", benchTest.Run())
	expectedBlockSize := int64(0.90 * float64(manifest.MaxBlockBytes))
	testnet.NoError("failed to check results", benchTest.CheckResults(expectedBlockSize))

	return nil
}

func LargeNetworkBigBlock64MB(logger *log.Logger) error {
	testName := "LargeNetworkBigBlock64MB"
	logger.Printf("Running %s\n", testName)
	manifest := bigBlockManifest

	manifest.MaxBlockBytes = 64 * mb
	manifest.Validators = 50
	manifest.TxClients = 50
	manifest.BlobSequences = 2

	manifest.ChainID = manifest.summary()
	log.Println("ChainID: ", manifest.ChainID)
	benchTest, err := NewBenchmarkTest(testName, &manifest)
	testnet.NoError("failed to create benchmark test", err)

	defer func() {
		log.Print("Cleaning up testnet")
		benchTest.Cleanup()
	}()

	testnet.NoError("failed to setup nodes", benchTest.SetupNodes())
	testnet.NoError("failed to run the benchmark test", benchTest.Run())
	expectedBlockSize := int64(0.90 * float64(manifest.MaxBlockBytes))
	testnet.NoError("failed to check results", benchTest.CheckResults(expectedBlockSize))

	return nil
}
