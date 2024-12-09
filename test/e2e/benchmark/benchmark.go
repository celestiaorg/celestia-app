//nolint:staticcheck
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/tendermint/tendermint/pkg/trace"

	"github.com/celestiaorg/celestia-app/v3/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v3/test/e2e/testnet"
	"github.com/celestiaorg/celestia-app/v3/test/util/testnode"
	"github.com/celestiaorg/knuu/pkg/knuu"
)

const timeFormat = "20060102_150405"

type BenchmarkTest struct {
	*testnet.Testnet
	manifest *Manifest
}

// NewBenchmarkTest wraps around testnet.New to create a new benchmark test.
// It may modify genesis consensus parameters based on manifest.
func NewBenchmarkTest(logger *log.Logger, name string, manifest *Manifest) (*BenchmarkTest, error) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	scope := fmt.Sprintf("%s_%s", name, time.Now().Format(timeFormat))
	kn, err := knuu.New(ctx, knuu.Options{
		Scope:        scope,
		ProxyEnabled: true,
	})
	if err != nil {
		return nil, err
	}

	// context.Background() is used to allow the stopSignal to be functional even after this function returns
	kn.HandleStopSignal(context.Background())

	log.Printf("Knuu initialized with scope %s", kn.Scope)

	testNet, err := testnet.New(logger, kn, testnet.Options{
		Grafana:          testnet.GetGrafanaInfoFromEnvVar(logger),
		ChainID:          manifest.ChainID,
		GenesisModifiers: manifest.GetGenesisModifiers(),
	})
	testnet.NoError("failed to create testnet", err)

	testNet.SetConsensusParams(manifest.GetConsensusParams())
	return &BenchmarkTest{Testnet: testNet, manifest: manifest}, nil
}

// SetupNodes creates genesis nodes and tx clients based on the manifest.
// There will be manifest.Validators many validators and manifest.TxClients many tx clients.
// Each tx client connects to one validator. If TxClients are fewer than Validators, some validators will not have a tx client.
func (b *BenchmarkTest) SetupNodes() error {
	ctx := context.Background()
	testnet.NoError("failed to create genesis nodes",
		b.CreateGenesisNodes(ctx, b.manifest.Validators,
			b.manifest.CelestiaAppVersion, b.manifest.SelfDelegation,
			b.manifest.UpgradeHeight, b.manifest.ValidatorResource, b.manifest.DisableBBR))

	// enable latency if specified in the manifest
	if b.manifest.EnableLatency {
		for _, node := range b.Nodes() {
			node.EnableNetShaper()
		}
	}
	// obtain the GRPC endpoints of the validators
	gRPCEndpoints, err := b.RemoteGRPCEndpoints(ctx)
	testnet.NoError("failed to get validators GRPC endpoints", err)
	log.Println("validators GRPC endpoints", gRPCEndpoints)

	// create tx clients and point them to the validators
	log.Println("Creating tx clients")

	err = b.CreateTxClients(
		ctx,
		b.manifest.TxClientVersion,
		b.manifest.BlobSequences,
		b.manifest.BlobSizes,
		b.manifest.BlobsPerSeq,
		b.manifest.TxClientsResource,
		gRPCEndpoints,
		map[int64]uint64{}, // upgrade schedule
	)
	testnet.NoError("failed to create tx clients", err)

	log.Println("Setting up testnet")
	testnet.NoError("failed to setup testnet", b.Setup(ctx,
		testnet.WithPerPeerBandwidth(b.manifest.PerPeerBandwidth),
		testnet.WithTimeoutPropose(b.manifest.TimeoutPropose),
		testnet.WithTimeoutCommit(b.manifest.TimeoutCommit),
		testnet.WithPrometheus(b.manifest.Prometheus),
		testnet.WithLocalTracing(b.manifest.LocalTracingType),
		testnet.WithTxIndexer("kv"),
		testnet.WithMempoolMaxTxsBytes(1*testnet.GiB),
		testnet.WithMempoolMaxTxBytes(8*testnet.MiB),
	))
	if b.manifest.PushTrace {
		log.Println("reading trace push config")
		if pushConfig, err := trace.GetPushConfigFromEnv(); err == nil {
			log.Print("Setting up trace push config")
			envVars := map[string]string{
				trace.PushBucketName: pushConfig.BucketName,
				trace.PushRegion:     pushConfig.Region,
				trace.PushAccessKey:  pushConfig.AccessKey,
				trace.PushKey:        pushConfig.SecretKey,
				trace.PushDelay:      fmt.Sprintf("%d", pushConfig.PushDelay),
			}
			for _, node := range b.Nodes() {
				for key, value := range envVars {
					if err = node.Instance.Build().SetEnvironmentVariable(key, value); err != nil {
						return fmt.Errorf("failed to set %s: %v", key, err)
					}
				}
			}
		}
	}
	return nil
}

// Run runs the benchmark test for the specified duration in the manifest.
func (b *BenchmarkTest) Run(ctx context.Context) error {
	log.Println("Starting benchmark testnet")

	log.Println("Starting nodes")
	if err := b.StartNodes(ctx); err != nil {
		return fmt.Errorf("failed to start testnet: %v", err)
	}

	// add latency if specified in the manifest
	if b.manifest.EnableLatency {
		for _, node := range b.Nodes() {
			err := node.SetLatencyAndJitter(
				b.manifest.LatencyParams.Latency,
				b.manifest.LatencyParams.Jitter,
			)
			if err != nil {
				return fmt.Errorf("failed to set latency and jitter: %v", err)
			}
		}
	}

	// wait for the nodes to sync
	log.Println("Waiting for nodes to sync")
	if err := b.WaitToSync(ctx); err != nil {
		return err
	}

	// start tx clients
	log.Println("Starting tx clients")
	if err := b.StartTxClients(ctx); err != nil {
		return fmt.Errorf("failed to start tx clients: %v", err)
	}

	// wait some time for the tx clients to submit transactions
	time.Sleep(b.manifest.TestDuration)

	return nil
}

func (b *BenchmarkTest) CheckResults(expectedBlockSizeBytes int64) error {
	log.Println("Checking results")

	// if local tracing was enabled,
	// pull block summary table from one of the nodes to confirm tracing
	// has worked properly.
	if b.manifest.LocalTracingType == "local" {
		if _, err := b.Node(0).PullBlockSummaryTraces("."); err != nil {
			return fmt.Errorf("failed to pull traces: %w", err)
		}
	}

	// download traces from S3, if enabled
	if b.manifest.PushTrace && b.manifest.DownloadTraces {
		// download traces from S3
		pushConfig, err := trace.GetPushConfigFromEnv()
		if err != nil {
			return fmt.Errorf("failed to get push config: %w", err)
		}
		err = trace.S3Download("./traces/", b.manifest.ChainID,
			pushConfig)
		if err != nil {
			return fmt.Errorf("failed to download traces from S3: %w", err)
		}
	}

	log.Println("Reading blockchain headers")
	blockchain, err := testnode.ReadBlockchainHeaders(context.Background(),
		b.Node(0).AddressRPC())
	testnet.NoError("failed to read blockchain headers", err)

	targetSizeReached := false
	maxBlockSize := int64(0)
	for _, blockMeta := range blockchain {
		if appconsts.LatestVersion != blockMeta.Header.Version.App {
			return fmt.Errorf("expected app version %d, got %d", appconsts.LatestVersion, blockMeta.Header.Version.App)
		}
		size := int64(blockMeta.BlockSize)
		if size > maxBlockSize {
			maxBlockSize = size
		}
		if maxBlockSize >= expectedBlockSizeBytes {
			targetSizeReached = true
			break
		}
	}
	if !targetSizeReached {
		return fmt.Errorf("max reached block size is %d byte and is not within the expected range of %d  and %d bytes", maxBlockSize, expectedBlockSizeBytes, b.manifest.MaxBlockBytes)
	}

	return nil
}
