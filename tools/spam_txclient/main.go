package main

import (
	"context"
	cryptorand "crypto/rand"
	"errors"
	"fmt"
	"log"
	"math"
	"math/rand"
	"sync/atomic"
	"time"

	"github.com/celestiaorg/celestia-app/v6/app"
	"github.com/celestiaorg/celestia-app/v6/app/encoding"
	"github.com/celestiaorg/celestia-app/v6/pkg/user"
	v2 "github.com/celestiaorg/celestia-app/v6/pkg/user/v2"
	"github.com/celestiaorg/go-square/v3/share"
	"github.com/cosmos/cosmos-sdk/client/grpc/cmtservice"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	sdktypes "github.com/cosmos/cosmos-sdk/types"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type Config struct {
	MochaEndpoint   string
	BlobSizeKB      int
	IntervalMs      int
	TestDurationSec int
}

func main() {
	cfg := Config{
		MochaEndpoint:   "rpc-mocha.pops.one:9090",
		BlobSizeKB:      300,  // 300 KiB blobs
		IntervalMs:      1000, // Submit every 1 second
		TestDurationSec: 0,    // Run forever (0 = no timeout)
	}

	err := RunLoadTest(cfg)
	if err != nil && !errors.Is(err, context.Canceled) {
		log.Fatalf("Load test failed: %v", err)
	}
}

// RunLoadTest sets up the tx client, runs submissions, and reports results.
func RunLoadTest(cfg Config) error {
	log.Println("Setting up tx client v2 with sequential queue and connecting to a mocha node")

	// Global test context with optional timeout
	var ctx context.Context
	var cancel context.CancelFunc
	if cfg.TestDurationSec > 0 {
		ctx, cancel = context.WithTimeout(context.Background(), time.Duration(cfg.TestDurationSec)*time.Second)
		log.Printf("Running for %d seconds", cfg.TestDurationSec)
	} else {
		ctx, cancel = context.WithCancel(context.Background())
		log.Println("Running forever (Ctrl+C to stop)")
	}
	defer cancel()

	txClient, grpcConn, cleanupFunc, err := NewMochaTxClientV2(ctx, cfg)
	if err != nil {
		return fmt.Errorf("failed to set up tx client v2: %w", err)
	}
	defer cleanupFunc()

	var (
		txCounter            int64
		successfulBroadcasts atomic.Int64
		successfulConfirms   atomic.Int64
		failedConfirms       atomic.Int64
		failedBroadcasts     atomic.Int64
		lastSuccess          atomic.Int64 // store UnixNano of last successful broadcast
	)
	// Initialize last success to now
	lastSuccess.Store(time.Now().UnixNano())

	g, ctx := errgroup.WithContext(ctx)
	ticker := time.NewTicker(time.Duration(cfg.IntervalMs) * time.Millisecond)
	defer ticker.Stop() // Clean up ticker

	// Submission loop which breaks out if the text duration is exceeded
	g.Go(func() error {
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-ticker.C:
				if time.Since(time.Unix(0, lastSuccess.Load())) > 10*time.Minute {
					return fmt.Errorf("TxClient appears halted: no successful submission in 10 minutes")
				}

				id := atomic.AddInt64(&txCounter, 1)

				// Separate goroutine for submitting txs via sequential queue
				g.Go(func() error {
					// Use background context so transaction never times out
					txCtx := context.Background()

					resp, err := SubmitPayForBlobSequential(txCtx, txClient, grpcConn, cfg.BlobSizeKB, id)
					if err != nil {
						fmt.Printf("\nTX-%d: Sequential submission failed: %v\n", id, err)
						failedBroadcasts.Add(1)
						return nil
					}

					lastSuccess.Store(time.Now().UnixNano())
					fmt.Printf("\nTX-%d: Sequential submission success (hash %s)\n", id, resp.TxHash)
					successfulBroadcasts.Add(1)

					// Sequential queue handles confirmation internally
					if resp.Code == 0 {
						fmt.Printf("\nTX-%d: Confirmed successfully: %s\n", id, resp.TxHash)
						successfulConfirms.Add(1)
					} else {
						fmt.Printf("\nTX-%d: Execution failed with code %d\n", id, resp.Code)
						failedConfirms.Add(1)
					}
					return nil
				})
			}
		}
	})

	// This should only fail if the client halts unexpectedly
	err = g.Wait()
	if err != nil && !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
		return fmt.Errorf("tx client v2 halted unexpectedly: %w", err)
	}

	fmt.Println("\n=== TxClient V2 Sequential Queue Test Results ===")
	fmt.Printf("Successful sequential submissions: %d\n", successfulBroadcasts.Load())
	fmt.Printf("Successful confirmations: %d\n", successfulConfirms.Load())
	fmt.Printf("Failed confirmations: %d\n", failedConfirms.Load())
	fmt.Printf("Failed submissions: %d\n", failedBroadcasts.Load())

	queueSize, err := txClient.GetSequentialQueueSize(txClient.DefaultAccountName())
	if err == nil {
		fmt.Printf("Final queue size: %d\n", queueSize)
	}

	cancel() // Clean up context before exit

	return nil
}

func SubmitPayForBlobSequential(ctx context.Context, txClient *v2.TxClient, grpcConn *grpc.ClientConn, blobSizeKB int, txID int64) (*sdktypes.TxResponse, error) {
	// Create random blob data of the given size
	blobData := make([]byte, blobSizeKB*1024)
	if _, err := cryptorand.Read(blobData); err != nil {
		return nil, err
	}

	namespace := share.RandomBlobNamespace()
	blob, err := share.NewV0Blob(namespace, blobData)
	if err != nil {
		return nil, err
	}

	currentHeight, err := getCurrentBlockHeight(ctx, grpcConn)
	if err != nil {
		return nil, err
	}

	// Set timeout height to be between 3 and 10 blocks from the current height
	timeoutHeight := currentHeight + int64(rand.Intn(8)+3)

	// Submit via sequential queue - this handles signing, broadcasting, and confirmation
	resp, err := txClient.SubmitPFBToSequentialQueue(ctx, []*share.Blob{blob}, user.SetTimeoutHeight(uint64(timeoutHeight)))
	if err != nil {
		return nil, err
	}

	return resp, nil
}

// getCurrentBlockHeight gets the current block height from the chain
func getCurrentBlockHeight(ctx context.Context, grpcConn *grpc.ClientConn) (int64, error) {
	// Give this call its own deadline (so it doesn't hang indefinitely)
	callCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	serviceClient := cmtservice.NewServiceClient(grpcConn)
	resp, err := serviceClient.GetLatestBlock(callCtx, &cmtservice.GetLatestBlockRequest{})
	if err != nil {
		return 0, fmt.Errorf("failed to get latest block: %w", err)
	}

	if resp == nil || resp.SdkBlock == nil {
		return 0, fmt.Errorf("failed to get latest block: response was incomplete")
	}

	return resp.SdkBlock.Header.Height, nil
}

func NewMochaTxClientV2(ctx context.Context, cfg Config) (*v2.TxClient, *grpc.ClientConn, func(), error) {
	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	kr, err := keyring.New(app.Name, keyring.BackendTest, "~/.celestia-app", nil, encCfg.Codec)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to initialize keyring: %w", err)
	}

	grpcConn, err := grpc.NewClient(
		cfg.MochaEndpoint,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(
			grpc.MaxCallSendMsgSize(math.MaxInt32),
			grpc.MaxCallRecvMsgSize(math.MaxInt32),
		),
	)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to create gRPC connection: %w", err)
	}

	clientCtx, cancel := context.WithCancel(ctx)

	// Create v2 TxClient with sequential queue
	txClient, err := v2.SetupTxClient(clientCtx, kr, grpcConn, encCfg)
	if err != nil {
		grpcConn.Close()
		cancel()
		return nil, nil, nil, fmt.Errorf("failed to create tx client v2: %w", err)
	}

	cleanup := func() {
		txClient.StopAllSequentialQueues()
		grpcConn.Close()
		cancel()
	}

	log.Printf("Sequential queue started for account: %s", txClient.DefaultAccountName())

	return txClient, grpcConn, cleanup, nil
}
