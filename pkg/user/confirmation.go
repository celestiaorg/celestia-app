package user

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/celestiaorg/celestia-app/v10/app/grpc/tx"
	"github.com/cometbft/cometbft/rpc/core"
	coregrpc "github.com/cometbft/cometbft/rpc/grpc"
	grpctypes "github.com/cosmos/cosmos-sdk/types/grpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const (
	maxConfirmationWaiters     = 1024
	confirmationBatchSize      = 20
	confirmationRPCTimeout     = 5 * time.Second
	confirmationSilenceTimeout = 30 * time.Second
)

type pendingConfirmation struct {
	done      chan struct{}
	response  *TxResponse
	err       error
	waiters   int
	dirty     bool
	evictedAt *time.Time
}

// txConfirmations owns one stream and coordinator. Registration and results are
// guarded by client.confirmationMu; only the coordinator handles transaction recovery.
type txConfirmations struct {
	client      *TxClient
	ctx         context.Context
	cancel      context.CancelFunc
	done        chan struct{}
	pending     map[string]*pendingConfirmation
	waiters     int
	wake        chan struct{}
	heights     chan struct{}
	reconnected chan struct{}
	failed      chan error
	height      atomic.Int64
}

// ConfirmTxSubscription waits for committed execution using shared height events.
// It requires app gRPC height subscriptions and committed query-height metadata.
func (client *TxClient) ConfirmTxSubscription(ctx context.Context, txHash string) (*TxResponse, error) {
	if len(txHash) != 64 {
		return nil, errors.New("transaction hash must contain 32 hex-encoded bytes")
	}
	decoded, err := hex.DecodeString(txHash)
	if err != nil || len(decoded) != 32 {
		return nil, errors.New("transaction hash must contain 32 hex-encoded bytes")
	}
	txHash = strings.ToUpper(txHash)
	for {
		client.confirmationMu.Lock()
		lifetime := client.confirmationContext
		if lifetime == nil {
			lifetime = context.Background()
		}
		if client.confirmationsClosed || lifetime.Err() != nil || ctx.Err() != nil {
			client.confirmationMu.Unlock()
			return nil, fmt.Errorf("transaction confirmation closed: %w", errors.Join(context.Canceled, lifetime.Err(), ctx.Err()))
		}
		s := client.confirmations
		if s != nil && s.ctx.Err() != nil {
			client.confirmationMu.Unlock()
			select {
			case <-s.done:
				continue
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		if s == nil {
			observerCtx, cancel := context.WithCancel(lifetime)
			s = &txConfirmations{
				client: client, ctx: observerCtx, cancel: cancel, done: make(chan struct{}),
				pending: make(map[string]*pendingConfirmation), wake: make(chan struct{}, 1),
				heights: make(chan struct{}, 1), reconnected: make(chan struct{}, 1), failed: make(chan error, 1),
			}
			client.confirmations = s
			go s.run()
		}
		if s.waiters == maxConfirmationWaiters {
			client.confirmationMu.Unlock()
			return nil, errors.New("transaction confirmation waiter limit reached")
		}
		p := s.pending[txHash]
		if p == nil {
			p = &pendingConfirmation{done: make(chan struct{}), dirty: true}
			s.pending[txHash] = p
			signalConfirmation(s.wake)
		}
		p.waiters++
		s.waiters++
		client.confirmationMu.Unlock()
		defer func() {
			client.confirmationMu.Lock()
			defer client.confirmationMu.Unlock()
			p.waiters--
			s.waiters--
			if p.waiters == 0 && s.pending[txHash] == p {
				delete(s.pending, txHash)
			}
			if len(s.pending) == 0 {
				s.cancel()
			}
		}()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-p.done:
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if p.response != nil {
				response := *p.response
				response.Signers = append([]string(nil), response.Signers...)
				return &response, p.err
			}
			return p.response, p.err
		}
	}
}

// CloseConfirmations stops subscription confirmations without closing the caller's
// gRPC connection or the legacy transaction queue. Future subscription calls fail.
func (client *TxClient) CloseConfirmations() {
	client.confirmationMu.Lock()
	client.confirmationsClosed = true
	s := client.confirmations
	if s != nil {
		s.cancel()
	}
	client.confirmationMu.Unlock()
	if s != nil {
		<-s.done
	}
}

func signalConfirmation(ch chan struct{}) {
	select {
	case ch <- struct{}{}:
	default:
	}
}

func (s *txConfirmations) readHeights() {
	api := coregrpc.NewBlockAPIClient(s.client.conns[0])
	for attempt := 0; attempt <= 3; attempt++ {
		if attempt > 0 {
			timer := time.NewTimer(time.Duration(1<<(attempt-1)) * 250 * time.Millisecond)
			select {
			case <-s.ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
		streamCtx, cancel := context.WithCancel(s.ctx)
		stream, err := api.SubscribeNewHeights(streamCtx, &coregrpc.SubscribeNewHeightsRequest{})
		if err == nil && attempt > 0 {
			signalConfirmation(s.reconnected)
		}
		for err == nil {
			var event *coregrpc.SubscribeNewHeightsResponse
			event, err = stream.Recv()
			if err == nil {
				if event.Height <= 0 {
					err = status.Error(codes.DataLoss, "invalid committed height notification")
					break
				}
				if event.Height > s.height.Load() {
					s.height.Store(event.Height)
					signalConfirmation(s.heights)
				}
			}
		}
		cancel()
		if s.ctx.Err() != nil {
			return
		}
		if attempt == 3 || (status.Code(err) != codes.Unavailable && status.Code(err) != codes.Unknown && !errors.Is(err, context.DeadlineExceeded)) {
			s.failed <- fmt.Errorf("height subscription unavailable: %w", err)
			return
		}
	}
}

func (s *txConfirmations) run() {
	readerDone := make(chan struct{})
	go func() { defer close(readerDone); s.readHeights() }()
	defer func() {
		s.cancel()
		<-readerDone
		s.client.confirmationMu.Lock()
		s.client.confirmations = nil
		s.client.confirmationMu.Unlock()
		close(s.done)
	}()
	timer := time.NewTimer(confirmationSilenceTimeout)
	defer timer.Stop()
	for {
		all, final := false, false
		var err error
		select {
		case <-s.ctx.Done():
			err = s.ctx.Err()
		case err = <-s.failed:
		case <-s.wake:
		case <-s.heights:
			all = true
			timer.Reset(confirmationSilenceTimeout)
		case <-s.reconnected:
			all = true
		case <-timer.C:
			all, final = true, true
		}
		if err == nil {
			err = s.refresh(all)
		}
		if final && err == nil {
			err = errors.New("transaction observation stopped: no committed height notifications for 30s")
		}
		if err != nil {
			s.client.confirmationMu.Lock()
			for hash, p := range s.pending {
				p.err = err
				close(p.done)
				delete(s.pending, hash)
			}
			s.cancel()
			s.client.confirmationMu.Unlock()
			return
		}
	}
}

func (s *txConfirmations) refresh(all bool) error {
	s.client.confirmationMu.Lock()
	hashes := make([]string, 0, len(s.pending))
	entries := make(map[string]*pendingConfirmation, len(s.pending))
	for hash, p := range s.pending {
		if all || p.dirty {
			hashes = append(hashes, hash)
			entries[hash] = p
			p.dirty = false
		}
	}
	s.client.confirmationMu.Unlock()
	ctx, cancel := context.WithTimeout(s.ctx, confirmationRPCTimeout)
	defer cancel()
	for start := 0; start < len(hashes); start += confirmationBatchSize {
		batch := hashes[start:min(start+confirmationBatchSize, len(hashes))]
		var header metadata.MD
		response, err := tx.NewTxClient(s.client.conns[0]).TxStatusBatch(ctx, &tx.TxStatusBatchRequest{TxIds: batch}, grpc.Header(&header))
		if err != nil {
			return fmt.Errorf("subscription transaction status: %w", err)
		}
		values := header.Get(grpctypes.GRPCBlockHeightHeader)
		if len(values) != 1 {
			return errors.New("subscription confirmation requires committed application height metadata")
		}
		committed, err := strconv.ParseInt(values[0], 10, 64)
		if err != nil || committed <= 0 {
			return errors.New("invalid committed application height metadata")
		}
		if len(response.Statuses) != len(batch) {
			return errors.New("invalid transaction status batch length")
		}
		for i, result := range response.Statuses {
			if result == nil || !strings.EqualFold(result.TxHash, batch[i]) || result.Status == nil {
				return errors.New("transaction status response does not match requested hash")
			}
			if result.Status.Status == core.TxStatusCommitted && result.Status.Height <= 0 {
				return errors.New("invalid transaction inclusion height")
			}
		}
		for i, result := range response.Statuses {
			hash, p := batch[i], entries[batch[i]]
			s.client.confirmationMu.Lock()
			active := s.pending[hash] == p
			s.client.confirmationMu.Unlock()
			if !active || (result.Status.Status == core.TxStatusCommitted && result.Status.Height > max(committed, s.height.Load())) {
				continue
			}
			resp, err := s.client.checkTxStatus(ctx, hash, result.Status, &p.evictedAt)
			if resp != nil || err != nil {
				s.client.confirmationMu.Lock()
				p.response, p.err = resp, err
				close(p.done)
				if s.pending[hash] == p {
					delete(s.pending, hash)
				}
				s.client.confirmationMu.Unlock()
			}
		}
	}
	return nil
}
