package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/celestiaorg/celestia-app/v3/app"
	"github.com/celestiaorg/celestia-app/v3/app/encoding"
	"github.com/celestiaorg/celestia-app/v3/test/e2e/testnet"
	"github.com/celestiaorg/celestia-app/v3/test/util/genesis"
	blobtypes "github.com/celestiaorg/celestia-app/v3/x/blob/types"
	"github.com/tendermint/tendermint/config"

	"github.com/tendermint/tendermint/pkg/trace"
	"github.com/tendermint/tendermint/pkg/trace/schema"
)

const (
	compactBlocksVersion = "c1a4ccf" //"a28b9e7"
)

func main() {
	if err := Run(); err != nil {
		log.Fatalf("failed to run experiment: %v", err)
	}
}

func Run() error {
	const (
		nodes          = 8
		timeoutCommit  = 3 * time.Second
		timeoutPropose = 4 * time.Second
		version        = compactBlocksVersion
	)

	blobParams := blobtypes.DefaultParams()
	// set the square size to 128
	blobParams.GovMaxSquareSize = 128
	ecfg := encoding.MakeConfig(app.ModuleBasics)

	network, err := testnet.New("compact-blocks", 864, nil, "", genesis.SetBlobParams(ecfg.Codec, blobParams))
	if err != nil {
		return err
	}
	defer network.Cleanup()

	cparams := app.DefaultConsensusParams()
	cparams.Block.MaxBytes = 8 * 1024 * 1024
	network.SetConsensusParams(cparams)

	err = network.CreateGenesisNodes(nodes, version, 10000000, 0, testnet.DefaultResources)
	if err != nil {
		return err
	}

	for _, node := range network.Nodes() {
		if err := node.Instance.EnableBitTwister(); err != nil {
			return fmt.Errorf("failed to enable bit twister: %v", err)
		}
	}

	gRPCEndpoints, err := network.RemoteGRPCEndpoints()
	if err != nil {
		return err
	}

	err = network.CreateTxClients(
		compactBlocksVersion,
		1,
		"128000-256000",
		1,
		testnet.DefaultResources,
		gRPCEndpoints[:3],
	)
	if err != nil {
		return err
	}

	log.Printf("Setting up network\n")
	err = network.Setup(
		testnet.WithTimeoutCommit(timeoutCommit),
		testnet.WithTimeoutPropose(timeoutPropose),
		testnet.WithMempool("v2"),
		func(cfg *config.Config) {
			// create a partially connected network by only dialing 5 peers
			cfg.P2P.MaxNumOutboundPeers = 3
			cfg.P2P.MaxNumInboundPeers = 4
			cfg.Instrumentation.TraceType = "local"
			cfg.Instrumentation.TracingTables = strings.Join([]string{
				schema.RoundStateTable,
				schema.BlockTable,
				schema.ProposalTable,
				schema.CompactBlockTable,
				schema.MempoolRecoveryTable,
			}, ",")
			cfg.LogLevel = "debug"
		},
	)
	if err != nil {
		return err
	}

	pushConfig, err := trace.GetPushConfigFromEnv()
	if err != nil {
		return err
	}
	log.Print("Setting up trace push config")
	for _, node := range network.Nodes() {
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

	log.Printf("Starting network\n")
	err = network.StartNodes()
	if err != nil {
		return err
	}

	for _, node := range network.Nodes() {
		if err = node.Instance.SetLatencyAndJitter(40, 10); err != nil {
			return fmt.Errorf("failed to set latency and jitter: %v", err)
		}
	}

	if err := network.WaitToSync(); err != nil {
		return err
	}

	if err := network.StartTxClients(); err != nil {
		return err
	}

	// run the test for 5 minutes
	heightTicker := time.NewTicker(20 * time.Second)
	timeout := time.NewTimer(5 * time.Minute)
	client, err := network.Node(0).Client()
	if err != nil {
		return err
	}
	for {
		select {
		case <-heightTicker.C:
			status, err := client.Status(context.Background())
			if err != nil {
				log.Printf("Error getting status: %v", err)
				continue
			}
			log.Printf("Height: %v", status.SyncInfo.LatestBlockHeight)

		case <-timeout.C:
			network.StopTxClients()
			log.Println("--- COLLECTING DATA")
			file := "/Users/callum/Developer/go/src/github.com/celestiaorg/big-blocks-research/traces"
			if err := trace.S3Download(file, network.ChainID(), pushConfig, schema.RoundStateTable, schema.BlockTable, schema.ProposalTable, schema.CompactBlockTable, schema.MempoolRecoveryTable); err != nil {
				return fmt.Errorf("failed to download traces from S3: %w", err)
			}

			log.Println("--- FINISHED ✅: ChainID: ", network.ChainID())
			return nil
		}
	}
}
