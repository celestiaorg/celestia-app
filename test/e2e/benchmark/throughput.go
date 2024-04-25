package main

import (
	"context"
	"fmt"
	"log"
	"math"
	"time"

	"github.com/celestiaorg/celestia-app/v2/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v2/test/e2e/testnets"
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

type BenchmarkTest struct {
	*testnets.Testnet
	manifest testnets.TestManifest
}

func NewBenchmarkTest(name string, manifest testnets.TestManifest) (
	*BenchmarkTest, error) {
	testnet, err := testnets.New(name, seed,
		testnets.GetGrafanaInfoFromEnvVar(), manifest.ChainID,
		manifest.GovMaxSquareSize, manifest.MaxBlockBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to create testnet: %w", err)
	}

	return &BenchmarkTest{
		Testnet:  testnet,
		manifest: manifest,
	}, nil
}

func (b *BenchmarkTest) Init() error {
	err := b.CreateGenesisNodes(b.manifest.Validators,
		b.manifest.CelestiaAppVersion, b.manifest.SelfDelegation,
		b.manifest.UpgradeHeight,
		b.manifest.ValidatorResource)

	if err != nil {
		return fmt.Errorf("failed to create genesis nodes %w", err)
	}

	// obtain the GRPC endpoints of the validators
	gRPCEndpoints, err := b.RemoteGRPCEndpoints()
	if err != nil {
		return fmt.Errorf("failed to get validators GRPC endpoints: %w", err)
	}
	log.Println("validators GRPC endpoints", gRPCEndpoints)

	// create txsim nodes and point them to the validators
	log.Println("Creating txsim nodes")

	err = b.CreateTxClients(b.manifest.TxClientVersion,
		b.manifest.BlobSequences,
		b.manifest.BlobSizes,
		b.manifest.TxClientsResource, gRPCEndpoints[:int64(math.Min(float64(
			b.manifest.Validators), float64(b.manifest.TxClientsNum)))])
	if err != nil {
		return fmt.Errorf("failed to create tx clients: %w", err)
	}

	// setup the testnet
	log.Println("Setting up testnet")
	err = b.Setup(testnets.BroadcastTxsOpt(b.manifest.BroadcastTxs),
		testnets.PrometheusOpt(b.manifest.Prometheus),
		testnets.MempoolOpt(b.manifest.Mempool),
		testnets.TimeoutCommitOpt(b.manifest.TimeoutCommit),
		testnets.TimeoutProposeOpt(b.manifest.TimeoutPropose),
		testnets.PerPeerBandwidthOpt(b.manifest.PerPeerBandwidth))

	if err != nil {
		return fmt.Errorf("failed to setup testnet: %w", err)
	}
	return nil
}

func (b *BenchmarkTest) Run() {
	log.Println("Starting tx clients (txsim)")
	testnets.NoError("failed to start tx clients", b.StartTxClients())

	// wait some time for the txsim to submit transactions
	time.Sleep(b.manifest.TestDuration)
}

func E2EThroughput() error {
	latestVersion, err := testnets.GetLatestVersion()
	testnets.NoError("failed to get latest version", err)

	log.Println("=== RUN E2EThroughput", "version:", latestVersion)

	// create test manifest
	manifest := testnets.TestManifest{
		ChainID:            "test-sanaz",
		Validators:         2,
		ValidatorResource:  testnets.DefaultResources,
		TxClientsResource:  testnets.DefaultResources,
		SelfDelegation:     10000000,
		CelestiaAppVersion: latestVersion,
		TxClientVersion:    testnets.TxsimVersion,
		BlobsPerSeq:        1,
		BlobSequences:      1,
		BlobSizes:          "100000",
		PerPeerBandwidth:   5 * 1024 * 1024,
		UpgradeHeight:      0,
		TimeoutCommit:      1 * time.Second,
		TimeoutPropose:     1 * time.Second,
		Mempool:            "v1",
		BroadcastTxs:       true,
		Prometheus:         true,
		GovMaxSquareSize:   appconsts.DefaultGovMaxSquareSize,
		MaxBlockBytes:      appconsts.DefaultMaxBytes,
		TestDuration:       10 * time.Second,
		TxClientsNum:       1,
	}

	benchTest, err := NewBenchmarkTest("E2EThroughput", manifest)
	testnets.NoError("failed to create benchmark testnet", err)

	testnets.NoError("failed to initialize the benchmark testnet", benchTest.Init())

	benchTest.Run()

	//testnet, err := testnets.New("E2EThroughput", seed,
	//	testnets.GetGrafanaInfoFromEnvVar(), manifest.ChainID,
	//	manifest.GovMaxSquareSize, manifest.MaxBlockBytes)
	//testnets.NoError("failed to create testnet", err)
	//
	defer func() {
		log.Print("Cleaning up testnet")
		benchTest.Cleanup()
	}()
	//
	//// add 2 validators
	//testnets.NoError("failed to create genesis nodes",
	//	testnet.CreateGenesisNodes(manifest.Validators,
	//		manifest.CelestiaAppVersion, manifest.SelfDelegation,
	//		manifest.UpgradeHeight,
	//		manifest.ValidatorResource))
	//
	//// obtain the GRPC endpoints of the validators
	//gRPCEndpoints, err := testnet.RemoteGRPCEndpoints()
	//testnets.NoError("failed to get validators GRPC endpoints", err)
	//log.Println("validators GRPC endpoints", gRPCEndpoints)
	//
	//// create txsim nodes and point them to the validators
	//log.Println("Creating txsim nodes")
	//
	//err = testnet.CreateTxClients(manifest.TxClientVersion, manifest.BlobSequences,
	//	manifest.BlobSizes,
	//	manifest.TxClientsResource, gRPCEndpoints[:int64(math.Min(float64(
	//		manifest.
	//		Validators),
	//		float64(manifest.TxClientsNum))])
	//testnets.NoError("failed to create tx clients", err)
	//
	//// start the testnet
	//log.Println("Setting up testnet")
	//testnets.NoError("failed to setup testnet",
	//	testnet.Setup(testnets.BroadcastTxsOpt(manifest.BroadcastTxs),
	//		testnets.PrometheusOpt(manifest.Prometheus),
	//		testnets.MempoolOpt(manifest.Mempool), testnets.TimeoutCommitOpt(
	//			manifest.TimeoutCommit), testnets.TimeoutProposeOpt(manifest.
	//			TimeoutPropose), testnets.PerPeerBandwidthOpt(manifest.
	//			PerPeerBandwidth)))
	//log.Println("Starting testnet")
	//testnets.NoError("failed to start testnet", testnet.Start())
	//
	//// once the testnet is up, start the txsim
	//log.Println("Starting txsim nodes")
	//testnets.NoError("failed to start tx clients", testnet.StartTxClients())
	//
	//// wait some time for the txsim to submit transactions
	//time.Sleep(manifest.TestDuration)

	log.Println("Reading blockchain")
	blockchain, err := testnode.ReadBlockchain(context.Background(),
		benchTest.Node(0).AddressRPC())
	testnets.NoError("failed to read blockchain", err)

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
