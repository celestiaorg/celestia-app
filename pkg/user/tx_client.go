package user

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	sdkmath "cosmossdk.io/math"
	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/cometbft/cometbft/rpc/core"
	"github.com/cosmos/cosmos-sdk/client"
	tmservice "github.com/cosmos/cosmos-sdk/client/grpc/cmtservice"
	nodeservice "github.com/cosmos/cosmos-sdk/client/grpc/node"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	sdktypes "github.com/cosmos/cosmos-sdk/types"
	sdktx "github.com/cosmos/cosmos-sdk/types/tx"
	paramtypes "github.com/cosmos/cosmos-sdk/x/params/types/proposal"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"

	"github.com/celestiaorg/go-square/v2/share"

	"github.com/celestiaorg/celestia-app/v4/app/encoding"
	"github.com/celestiaorg/celestia-app/v4/app/grpc/gasestimation"
	"github.com/celestiaorg/celestia-app/v4/app/grpc/tx"
	"github.com/celestiaorg/celestia-app/v4/app/params"
	"github.com/celestiaorg/celestia-app/v4/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v4/x/blob/types"
	minfeetypes "github.com/celestiaorg/celestia-app/v4/x/minfee/types"
)

const (
	DefaultPollTime          = 3 * time.Second
	txTrackerPruningInterval = 10 * time.Minute
)

type Option func(client *TxClient)

// txInfo is a struct that holds the sequence and the signer of a transaction
// in the local tx pool.
type txInfo struct {
	sequence  uint64
	signer    string
	timestamp time.Time
}

// TxResponse is a response from the chain after
// a transaction has been submitted.
type TxResponse struct {
	// Height is the block height at which the transaction was included on-chain.
	Height int64
	TxHash string
	Code   uint32
}

// BroadcastTxError is an error that occurs when broadcasting a transaction.
type BroadcastTxError struct {
	TxHash string
	Code   uint32
	// ErrorLog is the error output of the app's logger
	ErrorLog string
}

func (e *BroadcastTxError) Error() string {
	return fmt.Sprintf("broadcast tx error: %s", e.ErrorLog)
}

// ExecutionError is an error that occurs when a transaction gets executed.
type ExecutionError struct {
	TxHash string
	Code   uint32
	// ErrorLog is the error output of the app's logger
	ErrorLog string
}

func (e *ExecutionError) Error() string {
	return fmt.Sprintf("tx execution failed with code %d: %s", e.Code, e.ErrorLog)
}

// WithDefaultGasPrice sets the gas price.
func WithDefaultGasPrice(price float64) Option {
	return func(c *TxClient) {
		c.defaultGasPrice = price
	}
}

func WithPollTime(time time.Duration) Option {
	return func(c *TxClient) {
		c.pollTime = time
	}
}

func WithDefaultAddress(address sdktypes.AccAddress) Option {
	return func(c *TxClient) {
		record, err := c.signer.keys.KeyByAddress(address)
		if err != nil {
			panic(err)
		}
		c.defaultAccount = record.Name
		c.defaultAddress = address
	}
}

func WithDefaultAccount(name string) Option {
	return func(c *TxClient) {
		rec, err := c.signer.keys.Key(name)
		if err != nil {
			panic(err)
		}
		addr, err := rec.GetAddress()
		if err != nil {
			panic(err)
		}
		c.defaultAccount = name
		c.defaultAddress = addr
	}
}

// WithEstimatorService allows a user to provide a connection to a special gas
// estimation service to be used by the TxClient for estimating gas price
// and usage.
func WithEstimatorService(conn *grpc.ClientConn) Option {
	return func(c *TxClient) {
		c.gasEstimationClient = gasestimation.NewGasEstimatorClient(conn)
	}
}

// WithAdditionalCoreEndpoints adds additional core endpoints to the TxClient.
// For transaction submission, the client will attempt to use the primary endpoint
// and the first two additional endpoints provided via this option.
func WithAdditionalCoreEndpoints(conns []*grpc.ClientConn) Option {
	// set multiple core endpoints on the `TxClient`
	return func(c *TxClient) {
		c.conns = append(c.conns, conns...)
	}
}

// TxClient is an abstraction for building, signing, and broadcasting Celestia transactions
// It supports multiple accounts. If none is specified, it will
// try to use the default account.
// TxClient is thread-safe.
type TxClient struct {
	mtx      sync.Mutex
	cdc      codec.Codec
	signer   *Signer
	registry codectypes.InterfaceRegistry
	grpc     *grpc.ClientConn
	// list of core endpoints for tx submission (primary + additionals)
	conns []*grpc.ClientConn
	// how often to poll the network for confirmation of a transaction
	pollTime time.Duration
	// defaultGasPrice is the price used if no price is provided
	defaultGasPrice float64
	defaultAccount  string
	defaultAddress  sdktypes.AccAddress
	// txTracker maps the tx hash to the Sequence and signer of the transaction
	// that was submitted to the chain
	txTracker           map[string]txInfo
	gasEstimationClient gasestimation.GasEstimatorClient
}

// NewTxClient returns a new signer using the provided keyring
func NewTxClient(
	cdc codec.Codec,
	signer *Signer,
	conn *grpc.ClientConn,
	registry codectypes.InterfaceRegistry,
	options ...Option,
) (*TxClient, error) {
	records, err := signer.keys.List()
	if err != nil {
		return nil, fmt.Errorf("retrieving keys: %w", err)
	}

	if len(records) == 0 {
		return nil, errors.New("signer must have at least one key")
	}

	addr, err := records[0].GetAddress()
	if err != nil {
		return nil, err
	}

	txClient := &TxClient{
		signer:              signer,
		registry:            registry,
		grpc:                conn,
		conns:               []*grpc.ClientConn{conn},
		pollTime:            DefaultPollTime,
		defaultGasPrice:     appconsts.DefaultMinGasPrice,
		defaultAccount:      records[0].Name,
		defaultAddress:      addr,
		txTracker:           make(map[string]txInfo),
		cdc:                 cdc,
		gasEstimationClient: gasestimation.NewGasEstimatorClient(conn),
	}

	for _, opt := range options {
		opt(txClient)
	}

	return txClient, nil
}

// SetupTxClient uses the underlying grpc connection to populate the chainID, accountNumber and sequence number of all
// the accounts in the keyring.
func SetupTxClient(
	ctx context.Context,
	keys keyring.Keyring,
	conn *grpc.ClientConn,
	encCfg encoding.Config,
	options ...Option,
) (*TxClient, error) {
	resp, err := tmservice.NewServiceClient(conn).GetLatestBlock(
		ctx,
		&tmservice.GetLatestBlockRequest{},
	)
	if err != nil {
		return nil, err
	}

	chainID := resp.SdkBlock.Header.ChainID

	records, err := keys.List()
	if err != nil {
		return nil, err
	}

	accounts := make([]*Account, 0, len(records))
	for _, record := range records {
		addr, err := record.GetAddress()
		if err != nil {
			return nil, err
		}
		accNum, seqNum, err := QueryAccount(ctx, conn, encCfg.InterfaceRegistry, addr)
		if err != nil {
			// skip over the accounts that don't exist in state
			continue
		}

		accounts = append(accounts, NewAccount(record.Name, accNum, seqNum))
	}

	// query the min gas price from the chain
	minPrice, err := QueryMinimumGasPrice(ctx, conn)
	if err != nil {
		return nil, fmt.Errorf("querying minimum gas price: %w", err)
	}
	options = append([]Option{WithDefaultGasPrice(minPrice)}, options...)

	signer, err := NewSigner(keys, encCfg.TxConfig, chainID, accounts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create signer: %w", err)
	}

	return NewTxClient(encCfg.Codec, signer, conn, encCfg.InterfaceRegistry, options...)
}

// SubmitPayForBlob forms a transaction from the provided blobs, signs it, and submits it to the chain.
// TxOptions may be provided to set the fee and gas limit.
func (client *TxClient) SubmitPayForBlob(ctx context.Context, blobs []*share.Blob, opts ...TxOption) (*TxResponse, error) {
	resp, err := client.BroadcastPayForBlob(ctx, blobs, opts...)
	if err != nil {
		return nil, err
	}

	return client.ConfirmTx(ctx, resp.TxHash)
}

// SubmitPayForBlobWithAccount forms a transaction from the provided blobs, signs it with the provided account, and submits it to the chain.
// TxOptions may be provided to set the fee and gas limit.
func (client *TxClient) SubmitPayForBlobWithAccount(ctx context.Context, account string, blobs []*share.Blob, opts ...TxOption) (*TxResponse, error) {
	resp, err := client.BroadcastPayForBlobWithAccount(ctx, account, blobs, opts...)
	if err != nil {
		return nil, err
	}

	return client.ConfirmTx(ctx, resp.TxHash)
}

// BroadcastPayForBlob signs and broadcasts a transaction to pay for blobs.
// It does not confirm that the transaction has been committed on chain.
// If no gas or gas price is set, it will estimate the gas and use
// the max effective gas price: max(localMinGasPrice, networkMinGasPrice).
func (client *TxClient) BroadcastPayForBlob(ctx context.Context, blobs []*share.Blob, opts ...TxOption) (*sdktypes.TxResponse, error) {
	return client.BroadcastPayForBlobWithAccount(ctx, client.defaultAccount, blobs, opts...)
}

func (client *TxClient) BroadcastPayForBlobWithAccount(ctx context.Context, account string, blobs []*share.Blob, opts ...TxOption) (*sdktypes.TxResponse, error) {
	client.mtx.Lock()
	defer client.mtx.Unlock()
	if err := client.checkAccountLoaded(ctx, account); err != nil {
		return nil, err
	}

	blobSizes := make([]uint32, len(blobs))
	for i, blob := range blobs {
		blobSizes[i] = uint32(len(blob.Data()))
	}

	gasLimit := uint64(float64(types.DefaultEstimateGas(blobSizes)))
	fee := uint64(math.Ceil(appconsts.DefaultMinGasPrice * float64(gasLimit)))
	// prepend calculated params, so it can be overwritten in case the user has specified it.
	opts = append([]TxOption{SetGasLimit(gasLimit), SetFee(fee)}, opts...)

	txBytes, _, err := client.signer.CreatePayForBlobs(account, blobs, opts...)
	if err != nil {
		return nil, err
	}

	return client.broadcastTx(ctx, txBytes, account)
}

// SubmitTx forms a transaction from the provided messages, signs it, and submits it to the chain. TxOptions
// may be provided to set the fee and gas limit.
func (client *TxClient) SubmitTx(ctx context.Context, msgs []sdktypes.Msg, opts ...TxOption) (*TxResponse, error) {
	resp, err := client.BroadcastTx(ctx, msgs, opts...)
	if err != nil {
		return nil, err
	}

	return client.ConfirmTx(ctx, resp.TxHash)
}

func (client *TxClient) BroadcastTx(ctx context.Context, msgs []sdktypes.Msg, opts ...TxOption) (*sdktypes.TxResponse, error) {
	client.mtx.Lock()
	defer client.mtx.Unlock()

	// prune transactions that are older than 10 minutes
	// pruning has to be done in broadcast, since users
	// might not always call ConfirmTx().
	client.pruneTxTracker()

	account, err := client.getAccountNameFromMsgs(msgs)
	if err != nil {
		return nil, err
	}

	if err := client.checkAccountLoaded(ctx, account); err != nil {
		return nil, err
	}

	txBuilder, err := client.signer.txBuilder(msgs, opts...)
	if err != nil {
		return nil, err
	}

	hasUserSetFee := false
	for _, coin := range txBuilder.GetTx().GetFee() {
		if coin.Denom == appconsts.BondDenom {
			hasUserSetFee = true
			break
		}
	}

	gasLimit := txBuilder.GetTx().GetGas()
	if gasLimit == 0 {
		if !hasUserSetFee {
			// add at least 1utia as fee to builder as it affects gas calculation.
			txBuilder.SetFeeAmount(sdktypes.NewCoins(sdktypes.NewCoin(appconsts.BondDenom, sdkmath.NewInt(1))))
		}
		gasLimit, err = client.estimateGas(ctx, txBuilder)
		if err != nil {
			return nil, err
		}
		txBuilder.SetGasLimit(gasLimit)
	}

	if !hasUserSetFee {
		fee := int64(math.Ceil(appconsts.DefaultMinGasPrice * float64(gasLimit)))
		txBuilder.SetFeeAmount(sdktypes.NewCoins(sdktypes.NewCoin(appconsts.BondDenom, sdkmath.NewInt(fee))))
	}

	account, _, err = client.signer.signTransaction(txBuilder)
	if err != nil {
		return nil, err
	}

	txBytes, err := client.signer.EncodeTx(txBuilder.GetTx())
	if err != nil {
		return nil, err
	}

	return client.broadcastTx(ctx, txBytes, account)
}

func (client *TxClient) broadcastTx(ctx context.Context, txBytes []byte, signer string) (*sdktypes.TxResponse, error) {
	numConns := len(client.conns)
	if numConns == 0 {
		return nil, errors.New("no connections available in TxClient")
	}

	// Select up to 3 connections for broadcasting: primary + first 2 additional
	broadcastConns := client.conns
	if numConns > 3 {
		broadcastConns = client.conns[:3]
	}

	var (
		respCh    = make(chan *sdktypes.TxResponse, 1)
		errCh     = make(chan error, len(broadcastConns))
		broadcast = func(conn *grpc.ClientConn, childCtx context.Context) {
			txServiceClient := sdktx.NewServiceClient(conn)
			resp, err := txServiceClient.BroadcastTx(
				childCtx,
				&sdktx.BroadcastTxRequest{
					Mode:    sdktx.BroadcastMode_BROADCAST_MODE_SYNC,
					TxBytes: txBytes,
				},
			)
			if err != nil {
				// Don't return error on context cancellation as it's expected
				if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
					errCh <- fmt.Errorf("broadcast via %s failed: %w", conn.Target(), err)
				}
				return
			}
			if resp.TxResponse.Code != abci.CodeTypeOK {
				broadcastTxErr := &BroadcastTxError{
					TxHash:   resp.TxResponse.TxHash,
					Code:     resp.TxResponse.Code,
					ErrorLog: resp.TxResponse.RawLog,
				}
				errCh <- fmt.Errorf("broadcast via %s resulted in error: %w", conn.Target(), broadcastTxErr)
				return
			}
			// Success! Send response (non-blocking due to buffered channel)
			select {
			case respCh <- resp.TxResponse:
			default: // Another routine already sent the response or context was cancelled
			}
		}
	)

	broadcastCtx, cancel := context.WithCancel(ctx)
	defer cancel() // Ensure cancel is called eventually if not explicitly done earlier

	g, childCtx := errgroup.WithContext(broadcastCtx)
	var successfulBroadcast sync.Once // Ensure only one success is processed

	for _, conn := range broadcastConns {
		conn := conn // Capture range variable
		g.Go(func() error {
			broadcast(conn, childCtx)
			// Returning nil because errors are handled via errCh.
			// The errgroup context handles cancellation signals.
			return nil
		})
	}

	var firstResp *sdktypes.TxResponse
	var finalErr error

	// done channel signals when all goroutines have completed.
	done := make(chan struct{})
	go func() {
		// Wait for all goroutines in the group to complete.
		// Assign groupErr within this goroutine, but handle it after the select.
		groupErr := g.Wait()
		if groupErr != nil && !errors.Is(groupErr, context.Canceled) {
			// If there was a non-cancellation error from the group itself and
			// finalErr wasn't already set by context timeout/cancellation,
			// store it. We need to be careful about races if multiple goroutines
			// returned non-nil, but errgroup should only return the first.
			// Using a lock or atomic might be safer if we expected multiple
			// actual errors returned from g.Go functions (which we don't here).
			if finalErr == nil { // Avoid overwriting a context error
				finalErr = groupErr
			}
		}
		close(done)
	}()

	select {
	case firstResp = <-respCh:
		// Success from one connection. Immediately cancel other in-flight requests.
		cancel()
	case <-childCtx.Done(): // Parent context cancelled or deadline exceeded before first success
		finalErr = childCtx.Err()
		// If the context finished, ensure respCh is drained if a response arrived concurrently
		select {
		case firstResp = <-respCh:
			cancel() // Also cancel if we got a response just as context finished
		default:
		}
	case <-done: // All goroutines finished without success or context cancellation
		// Proceed to error collection below
	}

	// Wait for the errgroup goroutine (which includes g.Wait()) to finish.
	// This ensures all broadcast goroutines have completed and potentially sent
	// errors to errCh before we try to close and read from it.
	<-done

	// If we fall through the select because <-done was triggered or context expired,
	// we still need to potentially drain respCh in case a response arrived concurrently.
	if firstResp == nil {
		select {
		case firstResp = <-respCh:
			// A response arrived very late, just as g.Wait() finished.
			successfulBroadcast.Do(cancel) // Ensure cancellation signal is sent only once
		default:
		}
	}

	// If we received a successful response (either from select or late drain)
	if firstResp != nil {
		successfulBroadcast.Do(func() {
			// Ensure cancellation happens if we got a late success after <-done
			cancel()
			client.txTracker[firstResp.TxHash] = txInfo{
				sequence:  client.signer.accounts[signer].Sequence(),
				signer:    signer,
				timestamp: time.Now(),
			}

			incErr := client.signer.IncrementSequence(signer)
			if incErr != nil {
				fmt.Printf("CRITICAL: Failed to increment sequence for signer %s after successful broadcast: %v\n", signer, incErr)
				// TODO: Wrap the increment error and return it?
				// finalErr = fmt.Errorf("increment sequencing failed after successful broadcast: %w", incErr)
				// firstResp = nil
			}
		})
		// Return the successful response
		return firstResp, nil // We prioritize returning the successful response
	}

	// If no success, collect actual errors from the channel (excluding cancellation)
	close(errCh)
	var broadcastErrs []string // Renamed to avoid confusion with finalErr
	for err := range errCh {
		broadcastErrs = append(broadcastErrs, err.Error())
	}

	// Prioritize returning the context error if it occurred (and wasn't just cancellation)
	if finalErr != nil && !errors.Is(finalErr, context.Canceled) {
		// Optionally wrap finalErr with broadcast details if they exist
		if len(broadcastErrs) > 0 {
			return nil, fmt.Errorf("context error (%w) occurred after broadcast attempts failed: %s", finalErr, strings.Join(broadcastErrs, "; "))
		}
		return nil, finalErr // Return the original context error (e.g., DeadlineExceeded)
	}

	// If no critical context error, return combined broadcast errors if any
	if len(broadcastErrs) > 0 {
		return nil, fmt.Errorf("all broadcast attempts failed: %s", strings.Join(broadcastErrs, "; "))
	}

	// If context was cancelled but no specific broadcast errors occurred
	if finalErr != nil { // At this point, finalErr must be context.Canceled
		return nil, finalErr
	}

	// Should ideally not be reached
	return nil, errors.New("broadcast failed with no specific errors reported and context not cancelled")
}

// pruneTxTracker removes transactions from the local tx tracker that are older than 10 minutes
func (client *TxClient) pruneTxTracker() {
	for hash, txInfo := range client.txTracker {
		if time.Since(txInfo.timestamp) >= txTrackerPruningInterval {
			delete(client.txTracker, hash)
		}
	}
}

// ConfirmTx periodically pings the provided node for the commitment of a transaction by its
// hash. It will continually loop until the context is cancelled, the tx is found or an error
// is encountered.
func (client *TxClient) ConfirmTx(ctx context.Context, txHash string) (*TxResponse, error) {
	// TODO (#4578): Implement logic to potentially use other connections if the primary fails.
	// For now, use the primary connection.
	txClient := tx.NewTxClient(client.grpc)

	pollTicker := time.NewTicker(client.pollTime)
	defer pollTicker.Stop()

	for {
		resp, err := txClient.TxStatus(ctx, &tx.TxStatusRequest{TxId: txHash})
		if err != nil {
			return nil, err
		}

		switch resp.Status {
		case core.TxStatusPending:
			// Continue polling if the transaction is still pending
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-pollTicker.C:
				continue
			}
		case core.TxStatusCommitted:
			txResponse := &TxResponse{
				Height: resp.Height,
				TxHash: txHash,
				Code:   resp.ExecutionCode,
			}
			if resp.ExecutionCode != abci.CodeTypeOK {
				executionErr := &ExecutionError{
					TxHash:   txHash,
					Code:     resp.ExecutionCode,
					ErrorLog: resp.Error,
				}
				client.deleteFromTxTracker(txHash)
				return nil, executionErr
			}
			client.deleteFromTxTracker(txHash)
			return txResponse, nil
		case core.TxStatusEvicted:
			return nil, client.handleEvictions(txHash)
		default:
			client.deleteFromTxTracker(txHash)
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, fmt.Errorf("transaction with hash %s not found; it was likely rejected", txHash)
		}
	}
}

// handleEvictions handles the scenario where a transaction is evicted from the mempool.
// It removes the evicted transaction from the local tx tracker without incrementing
// the signer's sequence.
func (client *TxClient) handleEvictions(txHash string) error {
	client.mtx.Lock()
	defer client.mtx.Unlock()
	// Get transaction from the local tx tracker
	txInfo, exists := client.txTracker[txHash]
	if !exists {
		return fmt.Errorf("tx: %s not found in tx client txTracker; likely failed during broadcast", txHash)
	}
	// The sequence should be rolled back to the sequence of the transaction that was evicted to be
	// ready for resubmission. All transactions with a later nonce will be kicked by the nodes tx pool.
	if err := client.signer.SetSequence(txInfo.signer, txInfo.sequence); err != nil {
		return fmt.Errorf("setting sequence: %w", err)
	}
	delete(client.txTracker, txHash)
	return fmt.Errorf("tx was evicted from the mempool")
}

// deleteFromTxTracker safely deletes a transaction from the local tx tracker.
func (client *TxClient) deleteFromTxTracker(txHash string) {
	client.mtx.Lock()
	defer client.mtx.Unlock()
	delete(client.txTracker, txHash)
}

// EstimateGas simulates the transaction, calculating the amount of gas that was
// consumed during execution.
// Deprecated: use EstimateGasPriceAndUsage
func (client *TxClient) EstimateGas(ctx context.Context, msgs []sdktypes.Msg, opts ...TxOption) (uint64, error) {
	client.mtx.Lock()
	defer client.mtx.Unlock()

	txBuilder, err := client.signer.txBuilder(msgs, opts...)
	if err != nil {
		return 0, err
	}

	return client.estimateGas(ctx, txBuilder)
}

// EstimateGasPriceAndUsage returns the estimated gas price based on the provided priority,
// and also the gas limit/used for the provided transaction.
// The gas limit is calculated by simulating the transaction and then calculating the amount of gas that was consumed during execution.
func (client *TxClient) EstimateGasPriceAndUsage(
	ctx context.Context,
	msgs []sdktypes.Msg,
	priority gasestimation.TxPriority,
	opts ...TxOption,
) (gasPrice float64, gasUsed uint64, err error) {
	client.mtx.Lock()
	defer client.mtx.Unlock()

	txBuilder, err := client.signer.txBuilder(msgs, opts...)
	if err != nil {
		return 0, 0, err
	}

	// add at least 1utia as fee to builder as it affects gas calculation.
	txBuilder.SetFeeAmount(sdktypes.NewCoins(sdktypes.NewCoin(appconsts.BondDenom, sdkmath.NewInt(1))))

	_, _, err = client.signer.signTransaction(txBuilder)
	if err != nil {
		return 0, 0, err
	}
	txBytes, err := client.signer.EncodeTx(txBuilder.GetTx())
	if err != nil {
		return 0, 0, err
	}
	resp, err := client.gasEstimationClient.EstimateGasPriceAndUsage(ctx, &gasestimation.EstimateGasPriceAndUsageRequest{
		TxPriority: priority,
		TxBytes:    txBytes,
	})
	if err != nil {
		return 0, 0, fmt.Errorf("failed to estimate gas price and usage: %w", err)
	}

	gasUsed = uint64(float64(resp.EstimatedGasUsed))

	return resp.EstimatedGasPrice, gasUsed, nil
}

// EstimateGasPrice calls the gas estimation endpoint to return the estimated gas price based on priority.
func (client *TxClient) EstimateGasPrice(ctx context.Context, priority gasestimation.TxPriority) (float64, error) {
	client.mtx.Lock()
	defer client.mtx.Unlock()

	resp, err := client.gasEstimationClient.EstimateGasPrice(ctx, &gasestimation.EstimateGasPriceRequest{
		TxPriority: priority,
	})
	if err != nil {
		return 0, err
	}
	return resp.EstimatedGasPrice, nil
}

func (client *TxClient) estimateGas(ctx context.Context, txBuilder client.TxBuilder) (uint64, error) {
	// add at least 1utia as fee to builder as it affects gas calculation.
	txBuilder.SetFeeAmount(sdktypes.NewCoins(sdktypes.NewCoin(appconsts.BondDenom, sdkmath.NewInt(1))))

	_, _, err := client.signer.signTransaction(txBuilder)
	if err != nil {
		return 0, err
	}
	txBytes, err := client.signer.EncodeTx(txBuilder.GetTx())
	if err != nil {
		return 0, err
	}
	resp, err := client.gasEstimationClient.EstimateGasPriceAndUsage(ctx, &gasestimation.EstimateGasPriceAndUsageRequest{TxBytes: txBytes})
	if err != nil {
		return 0, err
	}

	gasLimit := uint64(float64(resp.EstimatedGasUsed))
	return gasLimit, nil
}

// Account returns an account of the signer from the key name. Also returns a bool if the
// account exists.
// Thread-safe
func (client *TxClient) Account(name string) *Account {
	client.mtx.Lock()
	defer client.mtx.Unlock()
	acc, exists := client.signer.accounts[name]
	if !exists {
		return nil
	}
	return acc.Copy()
}

func (client *TxClient) AccountByAddress(address sdktypes.AccAddress) *Account {
	client.mtx.Lock()
	defer client.mtx.Unlock()
	return client.signer.AccountByAddress(address)
}

func (client *TxClient) DefaultAddress() sdktypes.AccAddress {
	return client.defaultAddress
}

func (client *TxClient) DefaultAccountName() string { return client.defaultAccount }

func (client *TxClient) checkAccountLoaded(ctx context.Context, account string) error {
	if _, exists := client.signer.accounts[account]; exists {
		return nil
	}
	record, err := client.signer.keys.Key(account)
	if err != nil {
		return fmt.Errorf("trying to find account %s on keyring: %w", account, err)
	}
	addr, err := record.GetAddress()
	if err != nil {
		return fmt.Errorf("retrieving address from keyring: %w", err)
	}
	// FIXME: have a less trusting way of getting the account number and sequence
	accNum, sequence, err := QueryAccount(ctx, client.grpc, client.registry, addr)
	if err != nil {
		return fmt.Errorf("querying account %s: %w", account, err)
	}
	return client.signer.AddAccount(NewAccount(account, accNum, sequence))
}

func (client *TxClient) getAccountNameFromMsgs(msgs []sdktypes.Msg) (string, error) {
	var addr sdktypes.AccAddress
	for _, msg := range msgs {
		signers, _, err := client.cdc.GetMsgV1Signers(msg)
		if err != nil {
			return "", fmt.Errorf("getting signers from message: %w", err)
		}
		if len(signers) != 1 {
			return "", fmt.Errorf("only one signer per transaction supported, got %d", len(signers))
		}
		if addr == nil {
			addr = signers[0]
		}
		if !bytes.Equal(addr, signers[0]) {
			return "", errors.New("not supported: got two different signers across multiple messages")
		}
	}
	record, err := client.signer.keys.KeyByAddress(addr)
	if err != nil {
		return "", err
	}
	return record.Name, nil
}

// GetTxFromTxTracker gets transaction info from the tx client's local tx tracker by its hash
func (client *TxClient) GetTxFromTxTracker(hash string) (sequence uint64, signer string, exists bool) {
	client.mtx.Lock()
	defer client.mtx.Unlock()
	txInfo, exists := client.txTracker[hash]
	return txInfo.sequence, txInfo.signer, exists
}

// Signer exposes the tx clients underlying signer
func (client *TxClient) Signer() *Signer {
	return client.signer
}

func (client *TxClient) SetDefaultGasPrice(price float64) {
	client.mtx.Lock()
	defer client.mtx.Unlock()
	client.defaultGasPrice = price
}

// QueryMinimumGasPrice queries both the nodes local and network wide
// minimum gas prices, returning the maximum of the two.
func QueryMinimumGasPrice(ctx context.Context, grpcConn *grpc.ClientConn) (float64, error) {
	cfgRsp, err := nodeservice.NewServiceClient(grpcConn).Config(ctx, &nodeservice.ConfigRequest{})
	if err != nil {
		return 0, err
	}

	localMinCoins, err := sdktypes.ParseDecCoins(cfgRsp.MinimumGasPrice)
	if err != nil {
		return 0, err
	}
	localMinPrice := localMinCoins.AmountOf(params.BondDenom).MustFloat64()

	networkMinPrice, err := QueryNetworkMinGasPrice(ctx, grpcConn)
	if err != nil {
		// check if the network version supports a global min gas
		// price using a regex check. If not (i.e. v1) use the
		// local price only
		if strings.Contains(err.Error(), "unknown subspace: minfee") {
			return localMinPrice, nil
		}
		return 0, err
	}

	// return the highest value of the two
	return max(localMinPrice, networkMinPrice), nil
}

func QueryNetworkMinGasPrice(ctx context.Context, grpcConn *grpc.ClientConn) (float64, error) {
	paramsClient := paramtypes.NewQueryClient(grpcConn)
	// NOTE: that we don't prove that this is the correct value
	paramResponse, err := paramsClient.Params(ctx, &paramtypes.QueryParamsRequest{Subspace: minfeetypes.ModuleName, Key: string(minfeetypes.KeyNetworkMinGasPrice)})
	if err != nil {
		return 0, fmt.Errorf("querying params module: %w", err)
	}

	var networkMinPrice float64
	// Value is empty if network min gas price is not supported i.e. v1 state machine.
	if paramResponse.Param.Value != "" {
		networkMinPrice, err = strconv.ParseFloat(strings.Trim(paramResponse.Param.Value, `"`), 64)
		if err != nil {
			return 0, fmt.Errorf("parsing network min gas price: %w", err)
		}
	}
	return networkMinPrice, nil
}
