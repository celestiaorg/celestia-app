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
)

func main() {
	if err := TwoNodeBigBlock_128MiB(); err != nil {
		log.Fatalf("--- ERROR TwoNode test: %v", err.Error())
	}
}

func Run(manifest *Manifest) error {

	log.Printf("=== RUN %s=== version:%s", manifest.TestName,
		manifest.CelestiaAppVersion)
	// create a new testnet
	testNet, err := testnet.New(manifest.TestName, seed,
		testnet.GetGrafanaInfoFromEnvVar(), manifest.ChainID,
		manifest.GetGenesisModifiers()...)
	testnet.NoError("failed to create testnet", err)

	testNet.SetConsensusParams(manifest.GetConsensusParams())

	defer func() {
		log.Print("Cleaning up testnet")
		testNet.Cleanup()
	}()

	testnet.NoError("failed to create genesis nodes",
		testNet.CreateGenesisNodes(manifest.Validators,
			manifest.CelestiaAppVersion, manifest.SelfDelegation,
			manifest.UpgradeHeight, manifest.ValidatorResource))

	if manifest.PushTrace {
		log.Println("reading trace push config")
		if pushConfig, err := trace.GetPushConfigFromEnv(); err == nil {
			log.Print("Setting up trace push config")
			for _, node := range testNet.Nodes() {
				testnet.NoError("failed to set TRACE_PUSH_BUCKET_NAME",
					node.Instance.SetEnvironmentVariable(trace.PushBucketName, pushConfig.BucketName))
				testnet.NoError("failed to set TRACE_PUSH_REGION",
					node.Instance.SetEnvironmentVariable(trace.PushRegion, pushConfig.Region))
				testnet.NoError("failed to set TRACE_PUSH_ACCESS_KEY",
					node.Instance.SetEnvironmentVariable(trace.PushAccessKey, pushConfig.AccessKey))
				testnet.NoError("failed to set TRACE_PUSH_SECRET_KEY",
					node.Instance.SetEnvironmentVariable(trace.PushKey, pushConfig.SecretKey))
				testnet.NoError("failed to set TRACE_PUSH_DELAY",
					node.Instance.SetEnvironmentVariable(trace.PushDelay, fmt.Sprintf("%d", pushConfig.PushDelay)))
			}
		}
	}

	// obtain the GRPC endpoints of the validators
	gRPCEndpoints, err := testNet.RemoteGRPCEndpoints()
	testnet.NoError("failed to get validators GRPC endpoints", err)
	log.Println("validators GRPC endpoints", gRPCEndpoints[:manifest.TxClients])

	// create tx clients and point them to the validators
	log.Println("Creating tx clients")

	err = testNet.CreateTxClients(manifest.TxClientVersion, manifest.BlobSequences,
		manifest.BlobSizes, manifest.BlobsPerSeq,
		manifest.TxClientsResource, gRPCEndpoints[:manifest.TxClients])
	testnet.NoError("failed to create tx clients", err)

	// start the testnet
	log.Println("Setting up testnet")
	testnet.NoError("failed to setup testnet", testNet.Setup(
		testnet.WithPerPeerBandwidth(manifest.PerPeerBandwidth),
		testnet.WithTimeoutPropose(manifest.TimeoutPropose),
		testnet.WithTimeoutCommit(manifest.TimeoutCommit),
		testnet.WithPrometheus(manifest.Prometheus),
		testnet.WithLocalTracing(manifest.LocalTracingType),
	))
	log.Println("Starting testnet")
	testnet.NoError("failed to start testnet", testNet.Start())

	// once the testnet is up, start the tx clients
	log.Println("Starting tx clients")
	testnet.NoError("failed to start tx clients", testNet.StartTxClients())

	// wait some time for the tx clients to submit transactions
	time.Sleep(manifest.TestDuration)

	// pull some traced tables from the nodes
	_, err = testNet.Node(0).PullRoundStateTraces()
	testnet.NoError("failed to pull round state traces", err)

	_, err = testNet.Node(0).PullReceivedBytes()
	testnet.NoError("failed to pull received bytes traces", err)

	log.Println("Reading blockchain")
	blockchain, err := testnode.ReadBlockchain(context.Background(), testNet.Node(0).AddressRPC())
	testnet.NoError("failed to read blockchain", err)

	err = SaveToCSV(extractHeaders(blockchain),
		fmt.Sprintf("./blockchain_%s.csv", manifest.TestName))
	if err != nil {
		log.Println("failed to save blockchain headers to a CSV file", err)
	}

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
	log.Println("--- PASS ✅: ", manifest.TestName)
	return nil
}
func TwoNodeSimple() error {
	manifest := Manifest{
		TestName:           "TwoNodeSimple",
		ChainID:            "two-node-simple",
		Validators:         2,
		ValidatorResource:  testnet.DefaultResources,
		TxClientsResource:  testnet.DefaultResources,
		SelfDelegation:     10000000,
		CelestiaAppVersion: "pr-3261",
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
		TxClients:          1,
		LocalTracingType:   "local",
		PushTrace:          false,
	}

	return Run(&manifest)
}

func TwoNodeBigBlock_128MiB() error {
	manifest := Manifest{
		TestName:   "TwoNodeBigBlock_128MiB",
		ChainID:    "test-sanaz",
		Validators: 2,
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
		BlobsPerSeq:        6,
		BlobSequences:      1,
		BlobSizes:          "200000",
		PerPeerBandwidth:   100 * 1024 * 1024,
		UpgradeHeight:      0,
		TimeoutCommit:      11 * time.Second,
		TimeoutPropose:     10 * time.Second,
		Mempool:            "v1", // ineffective as it always defaults to v1
		BroadcastTxs:       true,
		Prometheus:         true,
		GovMaxSquareSize:   1024,
		MaxBlockBytes:      128 * 1024 * 1024,
		TestDuration:       4 * time.Minute,
		TxClients:          2,
		LocalTracingType:   "local",
		PushTrace:          false,
	}
	return Run(&manifest)
}
