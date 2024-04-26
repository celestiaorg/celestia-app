package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/celestiaorg/celestia-app/v2/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v2/test/e2e/testnet"
	"github.com/celestiaorg/celestia-app/v2/test/util/testnode"
)

const (
	seed = 42
)

func main() {
	if err := E2EThroughput(); err != nil {
		log.Fatalf("--- ERROR Throughput test: %v", err.Error())
	}
}

type BenchTest struct {
	*testnet.Testnet
	manifest *testnet.Manifest
}

func NewBenchTest(name string, manifest *testnet.Manifest) (*BenchTest, error) {
	// create a new testnet
	testNet, err := testnet.New(name, seed,
		testnet.GetGrafanaInfoFromEnvVar(), manifest.ChainID,
		manifest.GetGenesisModifiers()...)
	if err != nil {
		return nil, err
	}

	testNet.SetConsensusParams(manifest.GetConsensusParams())
	return &BenchTest{Testnet: testNet, manifest: manifest}, nil
}

func (b *BenchTest) SetupNodes() error {
	testnet.NoError("failed to create genesis nodes",
		b.CreateGenesisNodes(b.manifest.Validators,
			b.manifest.CelestiaAppVersion, b.manifest.SelfDelegation,
			b.manifest.UpgradeHeight, b.manifest.ValidatorResource))

	// obtain the GRPC endpoints of the validators
	gRPCEndpoints, err := b.RemoteGRPCEndpoints()
	testnet.NoError("failed to get validators GRPC endpoints", err)
	log.Println("validators GRPC endpoints", gRPCEndpoints)

	// create txsim nodes and point them to the validators
	log.Println("Creating txsim nodes")

	err = b.CreateTxClients(b.manifest.TxClientVersion,
		b.manifest.BlobSequences,
		b.manifest.BlobSizes,
		b.manifest.TxClientsResource, gRPCEndpoints)
	testnet.NoError("failed to create tx clients", err)

	// start the testnet
	log.Println("Setting up testnet")
	testnet.NoError("failed to setup testnet", b.Setup(
		testnet.WithPerPeerBandwidth(b.manifest.PerPeerBandwidth),
		testnet.WithTimeoutPropose(b.manifest.TimeoutPropose),
		testnet.WithTimeoutCommit(b.manifest.TimeoutCommit),
		testnet.WithPrometheus(b.manifest.Prometheus),
	))
	return nil
}

func (b *BenchTest) Run() error {
	log.Println("Starting testnet")
	err := b.Start()
	if err != nil {
		return fmt.Errorf("failed to start testnet: %v", err)
	}

	// once the testnet is up, start the txsim
	log.Println("Starting tx clients")
	err = b.StartTxClients()
	if err != nil {
		return fmt.Errorf("failed to start tx clients: %v", err)
	}

	// wait some time for the txsim to submit transactions
	time.Sleep(b.manifest.TestDuration)

	// TODO perhaps we can stop the nodes at this point to save resources
	return nil
}

func E2EThroughput() error {
	latestVersion, err := testnet.GetLatestVersion()
	testnet.NoError("failed to get latest version", err)

	log.Println("=== RUN E2EThroughput", "version:", latestVersion)

	manifest := testnet.Manifest{
		ChainID:            "test-sanaz",
		Validators:         2,
		ValidatorResource:  testnet.DefaultResources,
		TxClientsResource:  testnet.DefaultResources,
		SelfDelegation:     10000000,
		CelestiaAppVersion: latestVersion,
		TxClientVersion:    testnet.TxsimVersion,
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
		TestDuration:       30 * time.Second,
		TxClients:          2,
	}
	benchTest, err := NewBenchTest("E2EThroughput", &manifest)
	testnet.NoError("failed to create bench test", err)
	defer func() {
		log.Print("Cleaning up testnet")
		benchTest.Cleanup()
	}()

	testnet.NoError("failed to setup nodes", benchTest.SetupNodes())

	testnet.NoError("failed to run the bench test", benchTest.Run())

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

	log.Println("--- PASS ✅: E2EThroughput")
	return nil
}
