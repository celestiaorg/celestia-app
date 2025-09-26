package main

import (
	"context"
	cryptorand "crypto/rand"
	"errors"
	"fmt"
	"log"
	"math"
	"math/rand"
	"strings"
	"sync/atomic"
	"time"

	"github.com/celestiaorg/celestia-app/v6/app"
	"github.com/celestiaorg/celestia-app/v6/app/encoding"
	"github.com/celestiaorg/celestia-app/v6/pkg/user"
	"github.com/celestiaorg/go-square/v3/share"
	cmtservice "github.com/cosmos/cosmos-sdk/client/grpc/cmtservice"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const (
	mochaEndpoint   = "rpc-mocha.pops.one:9090"
	blobSizeKB      = 300  // 300 KiB blobs
	intervalMs      = 1000 // Submit every 1 second
	testDurationSec = 60   // Run for 60 seconds
)

func main() {
	log.Println("Setting up tx client and connecting to mocha")

	txClient, grpcConn, cancel, err := NewMochaTxClient()
	if err != nil {
		log.Fatalf("failed to set up tx client: %v", err)
	}
	defer grpcConn.Close()
	defer cancel()

	ctx, testCancel := context.WithTimeout(context.Background(), testDurationSec*time.Second)
	defer testCancel()

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

	// Submission loop
	ticker := time.NewTicker(time.Duration(intervalMs) * time.Millisecond)
	defer ticker.Stop()

	g.Go(func() error {
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-ticker.C:
				// Health check first
				since := time.Since(time.Unix(0, lastSuccess.Load()))
				if since > 10*time.Second {
					return fmt.Errorf("TxClient appears halted: no successful submission for %v", since)
				}

				id := atomic.AddInt64(&txCounter, 1) // Tells us which transactions in the loop we are on
				go func(id int64) {
					hash, err := BroadcastPayForBlob(ctx, txClient, grpcConn, blobSizeKB, id)
					if err != nil || hash == "" {
						fmt.Printf("\nTX-%d: Broadcast failed: %v\n", id, err)
						failedBroadcasts.Add(1)
						return
					}

					lastSuccess.Store(time.Now().UnixNano())
					fmt.Printf("\nTX-%d: Broadcast success (hash %s)\n", id, hash)
					successfulBroadcasts.Add(1)

					// Confirm in background
					go func(hash string) {
						resp, err := txClient.ConfirmTx(context.Background(), hash)
						if err != nil {
							fmt.Printf("\nTX-%d: Confirm failed: %v\n", id, err)
							failedConfirms.Add(1)
							return
						}
						fmt.Printf("\nTX-%d: Confirm tx success for %s: %d\n", id, hash, resp.Code)
						successfulConfirms.Add(1)
					}(hash)
				}(id)
			}
		}
	})

	if err := g.Wait(); err != nil {
		if !errors.Is(err, context.DeadlineExceeded) {
			log.Fatalf("Script failed: %v", err)
		}
		fmt.Println("\nScript completed successfully!!")
		fmt.Printf("Successful broadcasts: %d\n", successfulBroadcasts.Load())
		fmt.Printf("Successful confirms: %d\n", successfulConfirms.Load())
		fmt.Printf("Failed confirms: %d\n", failedConfirms.Load())
		fmt.Printf("Failed broadcasts: %d\n", failedBroadcasts.Load())
	}
}

func BroadcastPayForBlob(ctx context.Context, txClient *user.TxClient, grpcConn *grpc.ClientConn, blobSizeKB int, txID int64) (hash string, err error) {
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
	resp, err := txClient.BroadcastPayForBlob(context.Background(), []*share.Blob{blob}, user.SetTimeoutHeight(uint64(timeoutHeight)))
	if err != nil {
		return "", err
	}

	return resp.TxHash, nil
}

// getCurrentBlockHeight gets the current block height from the chain
func getCurrentBlockHeight(ctx context.Context, grpcConn *grpc.ClientConn) (int64, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	serviceClient := cmtservice.NewServiceClient(grpcConn)
	resp, err := serviceClient.GetLatestBlock(queryCtx, &cmtservice.GetLatestBlockRequest{})
	if err != nil && !strings.Contains(err.Error(), "context deadline exceeded") {
		return 0, fmt.Errorf("failed to get latest block: %w", err)
	}

	return resp.SdkBlock.Header.Height, nil
}

func NewMochaTxClient() (*user.TxClient, *grpc.ClientConn, context.CancelFunc, error) {
	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	kr, err := keyring.New(app.Name, keyring.BackendTest, "~/.celestia-app", nil, encCfg.Codec)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to initialize keyring: %w", err)
	}

	grpcConn, err := grpc.NewClient(
		mochaEndpoint,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(
			grpc.MaxCallSendMsgSize(math.MaxInt32),
			grpc.MaxCallRecvMsgSize(math.MaxInt32),
		),
	)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to create gRPC connection: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	txClient, err := user.SetupTxClient(ctx, kr, grpcConn, encCfg)
	if err != nil {
		grpcConn.Close()
		cancel()
		return nil, nil, nil, fmt.Errorf("failed to create tx client: %w", err)
	}

	return txClient, grpcConn, cancel, nil
}
