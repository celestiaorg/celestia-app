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
	"github.com/celestiaorg/knuu/pkg/knuu"
	"github.com/tendermint/tendermint/config"

	"github.com/tendermint/tendermint/pkg/trace"
	"github.com/tendermint/tendermint/pkg/trace/schema"
)

const (
	compactBlocksVersion = "70e7354"
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
		timeFormat     = "20060102_150405"
	)

	blobParams := blobtypes.DefaultParams()
	// set the square size to 128
	blobParams.GovMaxSquareSize = 128
	ecfg := encoding.MakeConfig(app.ModuleBasics)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	identifier := fmt.Sprintf("%s_%s", "compact-blocks", time.Now().Format(timeFormat))
	kn, err := knuu.New(ctx, knuu.Options{
		Scope:        identifier,
		ProxyEnabled: true,
	})
	testnet.NoError("failed to initialize Knuu", err)

	network, err := testnet.New(kn, testnet.Options{
		GenesisModifiers: []genesis.Modifier{
			genesis.SetBlobParams(ecfg.Codec, blobParams),
		},
		ChainID: identifier,
	})
	if err != nil {
		return err
	}
	defer network.Cleanup(ctx)

	cparams := app.DefaultConsensusParams()
	cparams.Block.MaxBytes = 8 * 1024 * 1024
	network.SetConsensusParams(cparams)

	err = network.CreateGenesisNodes(ctx, nodes, version, 10000000, 0, testnet.DefaultResources, true)
	if err != nil {
		return err
	}

	gRPCEndpoints, err := network.RemoteGRPCEndpoints(ctx)
	if err != nil {
		return err
	}

	err = network.CreateTxClients(
		ctx,
		compactBlocksVersion,
		40,
		"128000-256000",
		1,
		testnet.DefaultResources,
		gRPCEndpoints[:2],
		map[int64]uint64{},
	)
	if err != nil {
		return err
	}

	log.Printf("Setting up network\n")
	err = network.Setup(
		ctx,
		testnet.WithTimeoutCommit(timeoutCommit),
		testnet.WithTimeoutPropose(timeoutPropose),
		testnet.WithMempool("v2"),
		func(cfg *config.Config) {
			// create a partially connected network by only dialing 5 peers
			cfg.P2P.MaxNumOutboundPeers = 3
			cfg.P2P.MaxNumInboundPeers = 4
			cfg.Mempool.MaxTxsBytes = 100 * 1024 * 1024
			cfg.Instrumentation.TraceType = "local"
			cfg.Instrumentation.TracingTables = strings.Join([]string{
				schema.RoundStateTable,
				schema.BlockTable,
				schema.ProposalTable,
				schema.CompactBlockTable,
				schema.MempoolRecoveryTable,
			}, ",")
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
		if err = node.Instance.Build().SetEnvironmentVariable(trace.PushBucketName, pushConfig.BucketName); err != nil {
			return fmt.Errorf("failed to set TRACE_PUSH_BUCKET_NAME: %v", err)
		}
		if err = node.Instance.Build().SetEnvironmentVariable(trace.PushRegion, pushConfig.Region); err != nil {
			return fmt.Errorf("failed to set TRACE_PUSH_REGION: %v", err)
		}
		if err = node.Instance.Build().SetEnvironmentVariable(trace.PushAccessKey, pushConfig.AccessKey); err != nil {
			return fmt.Errorf("failed to set TRACE_PUSH_ACCESS_KEY: %v", err)
		}
		if err = node.Instance.Build().SetEnvironmentVariable(trace.PushKey, pushConfig.SecretKey); err != nil {
			return fmt.Errorf("failed to set TRACE_PUSH_SECRET_KEY: %v", err)
		}
		if err = node.Instance.Build().SetEnvironmentVariable(trace.PushDelay, fmt.Sprintf("%d", pushConfig.PushDelay)); err != nil {
			return fmt.Errorf("failed to set TRACE_PUSH_DELAY: %v", err)
		}
	}

	log.Printf("Starting network\n")
	err = network.Start(ctx)
	if err != nil {
		return err
	}

	// run the test for 5 minutes
	heightTicker := time.NewTicker(20 * time.Second)
	timeout := time.NewTimer(5 * time.Minute)
	client, err := network.Node(0).Client()
	if err != nil {
		return err
	}
	log.Println("--- RUNNING TESTNET")
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
			log.Println("--- COLLECTING DATA")
			file := "/Users/callum/Developer/go/src/github.com/celestiaorg/big-blocks-research/traces"
			if err := trace.S3Download(file, identifier, pushConfig, schema.RoundStateTable, schema.BlockTable, schema.ProposalTable, schema.CompactBlockTable, schema.MempoolRecoveryTable); err != nil {
				return fmt.Errorf("failed to download traces from S3: %w", err)
			}

			log.Println("--- FINISHED ✅: ChainID: ", identifier)
			return nil
		}
	}
}
