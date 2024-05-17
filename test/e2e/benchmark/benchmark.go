package main

import (
	"fmt"
	"log"
	"time"

	"github.com/celestiaorg/celestia-app/v2/test/e2e/testnet"
)

type BenchmarkTest struct {
	*testnet.Testnet
	manifest *Manifest
}

func NewBenchmarkTest(name string, manifest *Manifest) (*BenchmarkTest, error) {
	// create a new testnet
	testNet, err := testnet.New(name, seed,
		testnet.GetGrafanaInfoFromEnvVar(), manifest.ChainID,
		manifest.GetGenesisModifiers()...)
	if err != nil {
		return nil, err
	}

	testNet.SetConsensusParams(manifest.GetConsensusParams())
	return &BenchmarkTest{Testnet: testNet, manifest: manifest}, nil
}

// SetupNodes creates genesis nodes and tx clients based on the manifest.
// There will be manifest.Validators validators and manifest.TxClients tx clients.
// Each tx client connects to one validator. If TxClients are fewer than Validators, some validators will not have a tx client.
func (b *BenchmarkTest) SetupNodes() error {
	testnet.NoError("failed to create genesis nodes",
		b.CreateGenesisNodes(b.manifest.Validators,
			b.manifest.CelestiaAppVersion, b.manifest.SelfDelegation,
			b.manifest.UpgradeHeight, b.manifest.ValidatorResource))

	// obtain the GRPC endpoints of the validators
	gRPCEndpoints, err := b.RemoteGRPCEndpoints()
	testnet.NoError("failed to get validators GRPC endpoints", err)
	log.Println("validators GRPC endpoints", gRPCEndpoints)

	// create tx clients and point them to the validators
	log.Println("Creating tx clients")

	err = b.CreateTxClients(b.manifest.TxClientVersion,
		b.manifest.BlobSequences,
		b.manifest.BlobSizes,
		b.manifest.TxClientsResource, gRPCEndpoints)
	testnet.NoError("failed to create tx clients", err)

	// set up the testnet
	log.Println("Setting up testnet")
	testnet.NoError("failed to setup testnet", b.Setup(
		testnet.WithPerPeerBandwidth(b.manifest.PerPeerBandwidth),
		testnet.WithTimeoutPropose(b.manifest.TimeoutPropose),
		testnet.WithTimeoutCommit(b.manifest.TimeoutCommit),
		testnet.WithPrometheus(b.manifest.Prometheus),
	))
	return nil
}

// Run runs the benchmark test for the specified duration in the manifest.
func (b *BenchmarkTest) Run() error {
	log.Println("Starting testnet")
	err := b.Start()
	if err != nil {
		return fmt.Errorf("failed to start testnet: %v", err)
	}

	// once the testnet is up, start tx clients
	log.Println("Starting tx clients")
	err = b.StartTxClients()
	if err != nil {
		return fmt.Errorf("failed to start tx clients: %v", err)
	}

	// wait some time for the tx clients to submit transactions
	time.Sleep(b.manifest.TestDuration)

	return nil
}
