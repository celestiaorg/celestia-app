package user

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/celestiaorg/celestia-app/v11/app/grpc/tx"
	"github.com/cometbft/cometbft/rpc/core"
	coregrpc "github.com/cometbft/cometbft/rpc/grpc"
	grpctypes "github.com/cosmos/cosmos-sdk/types/grpc"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

const (
	confirmationBatchSize       = 20
	confirmationParallelBatches = 4
	confirmationBatchTimeout    = 5 * time.Second
	// confirmationFallbackInterval is how long a waiter goes without a status
	// from the feed before checking its own transaction directly.
	confirmationFallbackInterval = 30 * time.Second
	confirmationMinBackoff       = 250 * time.Millisecond
	confirmationMaxBackoff       = 5 * time.Second
)

// ConfirmTxSubscription waits for committed execution like ConfirmTx, but checks
// the transaction on every new block instead of on a fixed poll interval.
// Callers share one height subscription, which fetches their statuses in batches.
// If the subscription is unavailable, each caller falls back to checking its own
// status every 30 seconds.
func (client *TxClient) ConfirmTxSubscription(ctx context.Context, txHash string) (*TxResponse, error) {
	if _, err := hex.DecodeString(txHash); err != nil || len(txHash) != 64 {
		return nil, errors.New("transaction hash must contain 32 hex-encoded bytes")
	}
	txHash = strings.ToUpper(txHash)
	statuses, release := client.subscribeStatus(txHash)
	defer release()

	// Fires immediately so the first check doesn't wait for the next block.
	fallback := time.NewTimer(0)
	defer fallback.Stop()
	var evictedAt time.Time
	for {
		var status *tx.TxStatusResponse
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case status = <-statuses:
		case <-fallback.C:
			var err error
			if status, err = client.committedTxStatus(ctx, txHash); err != nil {
				return nil, err
			}
		}
		fallback.Reset(confirmationFallbackInterval)
		if status == nil {
			continue
		}
		if resp, err := client.checkTxStatus(ctx, txHash, status, &evictedAt); resp != nil || err != nil {
			return resp, err
		}
	}
}

// committedTxStatus queries a single transaction status, returning nil while a
// committed result is not yet visible at the application's committed height.
func (client *TxClient) committedTxStatus(ctx context.Context, txHash string) (*tx.TxStatusResponse, error) {
	var header metadata.MD
	resp, err := tx.NewTxClient(client.conns[0]).TxStatus(ctx, &tx.TxStatusRequest{TxId: txHash}, grpc.Header(&header))
	if err != nil {
		return nil, fmt.Errorf("transaction status: %w", err)
	}
	visible, err := isCommittedVisible(resp, header)
	if err != nil || !visible {
		return nil, err
	}
	return resp, nil
}

// isCommittedVisible reports whether a status can be acted on. A committed
// transaction is only reported once the application has committed its height,
// so state queries that follow confirmation observe its effects.
func isCommittedVisible(status *tx.TxStatusResponse, header metadata.MD) (bool, error) {
	values := header.Get(grpctypes.GRPCBlockHeightHeader)
	if len(values) != 1 {
		return false, errors.New("subscription confirmation requires committed application height metadata")
	}
	committed, err := strconv.ParseInt(values[0], 10, 64)
	if err != nil || committed <= 0 {
		return false, errors.New("invalid committed application height metadata")
	}
	if status.Status != core.TxStatusCommitted {
		return true, nil
	}
	if status.Height <= 0 {
		return false, errors.New("invalid transaction inclusion height")
	}
	return status.Height <= committed, nil
}

// statusFeed shares one height subscription between ConfirmTxSubscription
// callers and, on every new height, fetches their statuses in batches. It stops
// when the last caller leaves.
type statusFeed struct {
	client *TxClient
	cancel context.CancelFunc
	// waiters is guarded by client.confirmationMu.
	waiters map[string][]chan *tx.TxStatusResponse
}

// subscribeStatus registers a waiter for txHash and starts the feed if needed.
// The returned channel always holds the latest status the feed has seen.
func (client *TxClient) subscribeStatus(txHash string) (<-chan *tx.TxStatusResponse, func()) {
	ch := make(chan *tx.TxStatusResponse, 1)
	client.confirmationMu.Lock()
	defer client.confirmationMu.Unlock()
	f := client.statusFeed
	if f == nil {
		ctx, cancel := context.WithCancel(context.Background())
		f = &statusFeed{client: client, cancel: cancel, waiters: make(map[string][]chan *tx.TxStatusResponse)}
		client.statusFeed = f
		go f.run(ctx)
	}
	f.waiters[txHash] = append(f.waiters[txHash], ch)

	release := func() {
		client.confirmationMu.Lock()
		defer client.confirmationMu.Unlock()
		f.waiters[txHash] = slices.DeleteFunc(f.waiters[txHash], func(c chan *tx.TxStatusResponse) bool { return c == ch })
		if len(f.waiters[txHash]) == 0 {
			delete(f.waiters, txHash)
		}
		if len(f.waiters) == 0 {
			f.cancel()
			client.statusFeed = nil
		}
	}
	return ch, release
}

func (f *statusFeed) run(ctx context.Context) {
	heights := make(chan struct{}, 1)
	go f.watchHeights(ctx, heights)
	for {
		select {
		case <-ctx.Done():
			return
		case <-heights:
			f.refresh(ctx)
		}
	}
}

// watchHeights signals heights on every new block, reconnecting with backoff
// for as long as the feed runs. Heights that arrive during a refresh coalesce.
func (f *statusFeed) watchHeights(ctx context.Context, heights chan<- struct{}) {
	api := coregrpc.NewBlockAPIClient(f.client.conns[0])
	backoff := confirmationMinBackoff
	for {
		stream, err := api.SubscribeNewHeights(ctx, &coregrpc.SubscribeNewHeightsRequest{})
		for err == nil {
			if _, err = stream.Recv(); err == nil {
				backoff = confirmationMinBackoff
				select {
				case heights <- struct{}{}:
				default:
				}
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff = min(2*backoff, confirmationMaxBackoff)
	}
}

// refresh fetches the status of every waiting transaction. A failed batch is
// skipped: its waiters get the next height's results or fall back to checking
// directly.
func (f *statusFeed) refresh(ctx context.Context) {
	f.client.confirmationMu.Lock()
	hashes := slices.Collect(maps.Keys(f.waiters))
	f.client.confirmationMu.Unlock()

	var g errgroup.Group
	g.SetLimit(confirmationParallelBatches)
	for batch := range slices.Chunk(hashes, confirmationBatchSize) {
		g.Go(func() error {
			f.fetchBatch(ctx, batch)
			return nil
		})
	}
	_ = g.Wait()
}

func (f *statusFeed) fetchBatch(ctx context.Context, batch []string) {
	ctx, cancel := context.WithTimeout(ctx, confirmationBatchTimeout)
	defer cancel()
	var header metadata.MD
	resp, err := tx.NewTxClient(f.client.conns[0]).TxStatusBatch(ctx, &tx.TxStatusBatchRequest{TxIds: batch}, grpc.Header(&header))
	if err != nil || len(resp.Statuses) != len(batch) {
		return
	}
	for i, result := range resp.Statuses {
		if result == nil || result.Status == nil || !strings.EqualFold(result.TxHash, batch[i]) {
			continue
		}
		if visible, err := isCommittedVisible(result.Status, header); err == nil && visible {
			f.deliver(batch[i], result.Status)
		}
	}
}

// deliver replaces any undelivered status with the latest one, so it never blocks.
func (f *statusFeed) deliver(txHash string, status *tx.TxStatusResponse) {
	f.client.confirmationMu.Lock()
	defer f.client.confirmationMu.Unlock()
	for _, ch := range f.waiters[txHash] {
		select {
		case <-ch:
		default:
		}
		ch <- status
	}
}
