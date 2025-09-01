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
	"github.com/celestiaorg/celestia-app/v6/app/encoding"
	"github.com/celestiaorg/celestia-app/v6/app/grpc/gasestimation"
	"github.com/celestiaorg/celestia-app/v6/app/grpc/tx"
	"github.com/celestiaorg/celestia-app/v6/app/params"
	"github.com/celestiaorg/celestia-app/v6/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v6/x/blob/types"
	minfeetypes "github.com/celestiaorg/celestia-app/v6/x/minfee/types"
	"github.com/celestiaorg/go-square/v2/share"
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
	"google.golang.org/grpc"
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
	txBytes   []byte
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
	return fmt.Sprintf("broadcast tx error: hash %s error: %s code: %d", e.TxHash, e.ErrorLog, e.Code)
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

// WithPollTime sets a custom polling interval with which to check if a transaction has been submitted
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
	// list of core endpoints for tx submission (primary + additional)
	conns []*grpc.ClientConn
	// how often to poll the network for confirmation of a transaction
	pollTime time.Duration
	// sets the default account with which to submit transactions
	defaultAccount string
	defaultAddress sdktypes.AccAddress
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
		conns:               []*grpc.ClientConn{conn},
		pollTime:            DefaultPollTime,
		defaultAccount:      records[0].Name,
		defaultAddress:      addr,
		txTracker:           make(map[string]txInfo),
		cdc:                 cdc,
		gasEstimationClient: gasestimation.NewGasEstimatorClient(conn),
	}

	for _, opt := range options {
		opt(txClient)
	}

	// Sanity check to ensure we don't have more than 3 connections
	if len(txClient.conns) > 3 {
		txClient.conns = txClient.conns[:3]
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

	if len(client.conns) > 1 {
		return client.broadcastMulti(ctx, txBytes, account)
	}
	return client.broadcastTxAndIncrementSequence(ctx, client.conns[0], txBytes, account)
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

	if len(client.conns) > 1 {
		return client.broadcastMulti(ctx, txBytes, account)
	}
	return client.broadcastTxAndIncrementSequence(ctx, client.conns[0], txBytes, account)
}

func (client *TxClient) broadcastTxAndIncrementSequence(ctx context.Context, conn *grpc.ClientConn, txBytes []byte, signer string) (*sdktypes.TxResponse, error) {
	resp, err := client.broadcastTx(ctx, conn, txBytes, signer)
	if err != nil {
		return nil, err
	}

	// save the sequence and signer of the transaction in the local txTracker
	// before the sequence is incremented
	client.trackTransaction(signer, resp.TxHash, txBytes)

	// after the transaction has been submitted, we can increment the
	// sequence of the signer
	if err := client.signer.IncrementSequence(signer); err != nil {
		return nil, fmt.Errorf("increment sequencing: %w", err)
	}

	return resp, nil
}

// broadcastTx resubmits a transaction that was evicted from the mempool.
// Unlike the initial broadcast, it doesn't increment the signer's sequence number.
func (client *TxClient) broadcastTx(ctx context.Context, conn *grpc.ClientConn, txBytes []byte, signer string) (*sdktypes.TxResponse, error) {
	txClient := sdktx.NewServiceClient(conn)
	resp, err := txClient.BroadcastTx(
		ctx,
		&sdktx.BroadcastTxRequest{
			Mode:    sdktx.BroadcastMode_BROADCAST_MODE_SYNC,
			TxBytes: txBytes,
		},
	)
	if err != nil {
		return nil, err
	}
	if resp.TxResponse.Code != abci.CodeTypeOK {
		broadcastTxErr := &BroadcastTxError{
			TxHash:   resp.TxResponse.TxHash,
			Code:     resp.TxResponse.Code,
			ErrorLog: resp.TxResponse.RawLog,
		}
		return nil, broadcastTxErr
	}

	return resp.TxResponse, nil
}

// broadcastMulti broadcasts the transaction to multiple connections concurrently
// and returns the response from the first successful broadcast.
func (client *TxClient) broadcastMulti(ctx context.Context, txBytes []byte, signer string) (*sdktypes.TxResponse, error) {
	respCh := make(chan *sdktypes.TxResponse, 1)
	errCh := make(chan error, len(client.conns))

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(len(client.conns))

	for _, conn := range client.conns {
		go func(conn *grpc.ClientConn) {
			defer wg.Done()

			resp, err := client.broadcastTxAndIncrementSequence(ctx, conn, txBytes, signer)
			if err != nil {
				errCh <- err
				return
			}

			// On first successful response, send it and cancel others
			select {
			case respCh <- resp:
				cancel()
			case <-ctx.Done():
			}
		}(conn)
	}

	// Wait for all attempts to finish
	wg.Wait()
	close(respCh)
	close(errCh)

	// Return first successful response, if any
	if resp, ok := <-respCh; ok {
		return resp, nil
	}

	// Otherwise, return the first error encountered
	errs := make([]error, 0, len(errCh))
	for err := range errCh {
		errs = append(errs, err)
	}
	return nil, errors.Join(errs...)
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
	txClient := tx.NewTxClient(client.conns[0])

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
			_, signer, exists := client.GetTxFromTxTracker(txHash)
			if !exists {
				return nil, fmt.Errorf("tx: %s not found in txTracker; likely failed during broadcast", txHash)
			}
			// Resubmit straight away in the event of eviction and keep polling until tx is committed
			_, err := client.broadcastTx(ctx, client.conns[0], client.txTracker[txHash].txBytes, signer)
			if err != nil {
				return nil, fmt.Errorf("resubmission for evicted tx with hash %s failed: %w", txHash, err)
			}
		case core.TxStatusRejected:
			sequence, signer, exists := client.GetTxFromTxTracker(txHash)
			if !exists {
				return nil, fmt.Errorf("tx: %s not found in tx client txTracker; likely failed during broadcast", txHash)
			}
			// Reset sequence to the rejected tx's sequence to enable resubmission
			// of subsequent transactions.
			if err := client.signer.SetSequence(signer, sequence); err != nil {
				return nil, fmt.Errorf("setting sequence: %w", err)
			}
			client.deleteFromTxTracker(txHash)
			return nil, fmt.Errorf("tx with hash %s was rejected by the node", txHash)
		default:
			client.deleteFromTxTracker(txHash)
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, fmt.Errorf("transaction with hash %s not found", txHash)
		}
	}
}

// deleteFromTxTracker safely deletes a transaction from the local tx tracker.
func (client *TxClient) deleteFromTxTracker(txHash string) {
	client.mtx.Lock()
	defer client.mtx.Unlock()
	delete(client.txTracker, txHash)
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
	resp, err := client.gasEstimationClient.EstimateGasPrice(ctx, &gasestimation.EstimateGasPriceRequest{
		TxPriority: priority,
	})
	if err != nil {
		return 0, err
	}
	return resp.EstimatedGasPrice, nil
}

// estimateGas returns an estimate for the gas used by this tx.
func (client *TxClient) estimateGas(ctx context.Context, txBuilder client.TxBuilder) (uint64, error) {
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

// AccountByAddress retrieves the Account associated with the specified address.
// returns nil if the account is not loaded or if an error occurred while loading.
func (client *TxClient) AccountByAddress(ctx context.Context, address sdktypes.AccAddress) *Account {
	client.mtx.Lock()
	defer client.mtx.Unlock()

	accountName := client.signer.accountNameByAddress(address)
	if accountName == "" {
		return nil
	}

	if err := client.checkAccountLoaded(ctx, accountName); err != nil {
		return nil
	}

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
	accNum, sequence, err := QueryAccount(ctx, client.conns[0], client.registry, addr)
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

// trackTransaction tracks a transaction in the tx client's local tx tracker.
func (client *TxClient) trackTransaction(signer, txHash string, txBytes []byte) {
	sequence := client.signer.Account(signer).Sequence()
	client.txTracker[txHash] = txInfo{
		sequence:  sequence,
		signer:    signer,
		timestamp: time.Now(),
		txBytes:   txBytes,
	}
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
