package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/celestiaorg/celestia-app/v3/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v3/test/e2e/testnet"
	"github.com/celestiaorg/celestia-app/v3/test/util/testnode"
	"github.com/tendermint/tendermint/pkg/trace"
)

type BenchmarkTest struct {
	*testnet.Testnet
	manifest *Manifest
}

func NewBenchmarkTest(ctx context.Context, name string, manifest *Manifest) (*BenchmarkTest, error) {
	// create a new testnet
	testNet, err := testnet.New(ctx, name, seed,
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
func (b *BenchmarkTest) SetupNodes(ctx context.Context) error {
	testnet.NoError("failed to create genesis nodes",
		b.CreateGenesisNodes(ctx, b.manifest.Validators,
			b.manifest.CelestiaAppVersion, b.manifest.SelfDelegation,
			b.manifest.UpgradeHeight, b.manifest.ValidatorResource))

	// enable latency if specified in the manifest
	if b.manifest.EnableLatency {
		for _, node := range b.Nodes() {
			if err := node.Instance.EnableBitTwister(); err != nil {
				return fmt.Errorf("failed to enable bit twister: %v", err)
			}
		}
	}
	// obtain the GRPC endpoints of the validators
	gRPCEndpoints, err := b.RemoteGRPCEndpoints()
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
	)
	testnet.NoError("failed to create tx clients", err)

	log.Println("Setting up testnet")
	testnet.NoError("failed to setup testnet", b.Setup(
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
			for _, node := range b.Nodes() {
				if err = node.Instance.SetEnvironmentVariable(trace.PushBucketName, pushConfig.BucketName); err != nil {
					return fmt.Errorf("failed to set TRACE_PUSH_BUCKET_NAME: %v", err)
				}
				if err = node.Instance.SetEnvironmentVariable(trace.PushRegion, pushConfig.Region); err != nil {
					return fmt.Errorf("failed to set TRACE_PUSH_REGION: %v", err)
				}
				if err = node.Instance.SetEnvironmentVariable(trace.PushAccessKey, pushConfig.AccessKey); err != nil {
					return fmt.Errorf("failed to set TRACE_PUSH_ACCESS_KEY: %v", err)
				}
				if err = node.Instance.SetEnvironmentVariable(trace.PushKey, pushConfig.SecretKey); err != nil {
					return fmt.Errorf("failed to set TRACE_PUSH_SECRET_KEY: %v", err)
				}
				if err = node.Instance.SetEnvironmentVariable(trace.PushDelay, fmt.Sprintf("%d", pushConfig.PushDelay)); err != nil {
					return fmt.Errorf("failed to set TRACE_PUSH_DELAY: %v", err)
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
	err := b.StartNodes()
	if err != nil {
		return fmt.Errorf("failed to start testnet: %v", err)
	}

	// add latency if specified in the manifest
	if b.manifest.EnableLatency {
		for _, node := range b.Nodes() {
			if err = node.Instance.SetLatencyAndJitter(b.manifest.LatencyParams.
				Latency, b.manifest.LatencyParams.Jitter); err != nil {
				return fmt.Errorf("failed to set latency and jitter: %v", err)
			}
		}
	}

	// wait for the nodes to sync
	log.Println("Waiting for nodes to sync")
	err = b.WaitToSync()
	if err != nil {
		return err
	}

	// start tx clients
	log.Println("Starting tx clients")
	err = b.StartTxClients(ctx)
	if err != nil {
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
