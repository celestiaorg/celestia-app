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
	"github.com/celestiaorg/go-square/v3/share"
	"github.com/cosmos/cosmos-sdk/client/grpc/cmtservice"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
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
		TestDurationSec: 240,  // Run for 240 seconds
	}

	err := RunLoadTest(cfg)
	if err != nil && !errors.Is(err, context.Canceled) {
		log.Fatalf("Load test failed: %v", err)
	}
}

// RunLoadTest sets up the tx client, runs submissions, and reports results.
func RunLoadTest(cfg Config) error {
	log.Println("Setting up tx client and connecting to a mocha node")

	// Global test context with configured timeout
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(cfg.TestDurationSec)*time.Second)
	defer cancel()

	txClient, grpcConn, _, err := NewMochaTxClient(ctx, cfg)
	if err != nil {
		return fmt.Errorf("failed to set up tx client: %w", err)
	}

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
				if time.Since(time.Unix(0, lastSuccess.Load())) > 10*time.Second {
					return fmt.Errorf("TxClient appears halted: no successful submission recently")
				}

				id := atomic.AddInt64(&txCounter, 1)

				// Separate goroutine for broadcasting and confirming txs
				g.Go(func() error {
					hash, err := BroadcastPayForBlob(ctx, txClient, grpcConn, cfg.BlobSizeKB, id)
					if err != nil || hash == "" {
						fmt.Printf("\nTX-%d: Broadcast failed: %v\n", id, err)
						failedBroadcasts.Add(1)
						return nil
					}

					lastSuccess.Store(time.Now().UnixNano())
					fmt.Printf("\nTX-%d: Broadcast success (hash %s)\n", id, hash)
					successfulBroadcasts.Add(1)

					resp, err := txClient.ConfirmTx(ctx, hash)
					if err != nil {
						fmt.Printf("\nTX-%d: Confirm failed: %v\n", id, err)
						failedConfirms.Add(1)
						return nil
					}

					fmt.Printf("\nTX-%d: Confirm success for %s: %d\n", id, hash, resp.Code)
					successfulConfirms.Add(1)
					return nil
				})
			}
		}
	})

	// This should only fail if the client halts
	err = g.Wait()
	if err != nil && !errors.Is(err, context.DeadlineExceeded) {
		return err
	}

	fmt.Println("\nScript completed successfully!!")
	fmt.Printf("Successful broadcasts: %d\n", successfulBroadcasts.Load())
	fmt.Printf("Successful confirms: %d\n", successfulConfirms.Load())
	fmt.Printf("Failed confirms: %d\n", failedConfirms.Load())
	fmt.Printf("Failed broadcasts: %d\n", failedBroadcasts.Load())

	cancel() // Clean up context before exit

	return nil
}

func BroadcastPayForBlob(ctx context.Context, txClient *user.TxClient, grpcConn *grpc.ClientConn, blobSizeKB int, txID int64) (string, error) {
	// Create random blob data of the given size
	blobData := make([]byte, blobSizeKB*1024)
	if _, err := cryptorand.Read(blobData); err != nil {
		return "", err
	}

	namespace := share.RandomBlobNamespace()
	blob, err := share.NewV0Blob(namespace, blobData)
	if err != nil {
		return "", err
	}

	currentHeight, err := getCurrentBlockHeight(ctx, grpcConn)
	if err != nil {
		return "", err
	}

	// Set timeout height to be between 1 and 5 blocks from the current height
	timeoutHeight := currentHeight + int64(rand.Intn(5))
	resp, err := txClient.BroadcastPayForBlob(ctx, []*share.Blob{blob}, user.SetTimeoutHeight(uint64(timeoutHeight)))
	if err != nil {
		return "", err
	}

	return resp.TxHash, nil
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

func NewMochaTxClient(ctx context.Context, cfg Config) (*user.TxClient, *grpc.ClientConn, context.CancelFunc, error) {
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
	txClient, err := user.SetupTxClient(clientCtx, kr, grpcConn, encCfg)
	if err != nil {
		grpcConn.Close()
		cancel()
		return nil, nil, nil, fmt.Errorf("failed to create tx client: %w", err)
	}

	return txClient, grpcConn, cancel, nil
}
