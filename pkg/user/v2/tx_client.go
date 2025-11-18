package v2

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/celestiaorg/celestia-app/v6/app/encoding"
	"github.com/celestiaorg/celestia-app/v6/pkg/user"
	"github.com/celestiaorg/go-square/v3/share"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	sdktypes "github.com/cosmos/cosmos-sdk/types"
	"google.golang.org/grpc"
)

// TxClient is a v2 wrapper around the original TxClient that
// converts all user.TxResponse to sdktypes.TxResponse including the signer info.
type TxClient struct {
	// Embed the underlying client to automatically delegate all methods
	*user.TxClient

	// Sequential queues per account
	sequentialQueues map[string]*sequentialQueue
	queueMu          sync.RWMutex
}

// NewTxClient creates a new v2 TxClient by wrapping the original NewTxClient function.
func NewTxClient(
	cdc codec.Codec,
	signer *user.Signer,
	conn *grpc.ClientConn,
	registry codectypes.InterfaceRegistry,
	options ...user.Option,
) (*TxClient, error) {
	v1Client, err := user.NewTxClient(cdc, signer, conn, registry, options...)
	if err != nil {
		return nil, err
	}
	v2Client := &TxClient{
		TxClient:         v1Client,
		sequentialQueues: make(map[string]*sequentialQueue),
	}
	if err := v2Client.StartSequentialQueue(context.Background(), v1Client.DefaultAccountName()); err != nil {
		return nil, err
	}
	return v2Client, nil
}

// SetupTxClient creates and initializes a new v2 TxClient by wrapping the original setupTxClient method.
func SetupTxClient(
	ctx context.Context,
	keys keyring.Keyring,
	conn *grpc.ClientConn,
	encCfg encoding.Config,
	options ...user.Option,
) (*TxClient, error) {
	v1Client, err := user.SetupTxClient(ctx, keys, conn, encCfg, options...)
	if err != nil {
		return nil, err
	}
	v2Client := &TxClient{
		TxClient:         v1Client,
		sequentialQueues: make(map[string]*sequentialQueue),
	}
	if err := v2Client.StartSequentialQueue(ctx, v1Client.DefaultAccountName()); err != nil {
		return nil, err
	}
	return v2Client, nil
}

// Wrapv1TxClient wraps a v1 TxClient and returns a v2 TxClient.
func Wrapv1TxClient(v1Client *user.TxClient) *TxClient {
	return &TxClient{
		TxClient:         v1Client,
		sequentialQueues: make(map[string]*sequentialQueue),
	}
}

func (c *TxClient) buildSDKTxResponse(legacyResp *user.TxResponse) *sdktypes.TxResponse {
	return &sdktypes.TxResponse{
		Height:    legacyResp.Height,
		TxHash:    legacyResp.TxHash,
		Code:      legacyResp.Code,
		Codespace: legacyResp.Codespace,
		GasWanted: legacyResp.GasWanted,
		GasUsed:   legacyResp.GasUsed,
		Signers:   legacyResp.Signers,
	}
}

// Override only the methods that have breaking changes from the original TxClient.

// SubmitPayForBlob calls the original SubmitPayForBlob method and returns a v2 sdk.TxResponse.
func (c *TxClient) SubmitPayForBlob(ctx context.Context, blobs []*share.Blob, opts ...user.TxOption) (*sdktypes.TxResponse, error) {
	legacyResp, err := c.TxClient.SubmitPayForBlob(ctx, blobs, opts...)
	if err != nil {
		return nil, err
	}

	return c.buildSDKTxResponse(legacyResp), nil
}

// SubmitPayForBlobWithAccount calls the original SubmitPayForBlobWithAccount method and returns a v2 sdk.TxResponse.
func (c *TxClient) SubmitPayForBlobWithAccount(ctx context.Context, accountName string, blobs []*share.Blob, opts ...user.TxOption) (*sdktypes.TxResponse, error) {
	legacyResp, err := c.TxClient.SubmitPayForBlobWithAccount(ctx, accountName, blobs, opts...)
	if err != nil {
		return nil, err
	}

	return c.buildSDKTxResponse(legacyResp), nil
}

// SubmitTx calls the original SubmitTx method and returns a v2 sdk.TxResponse.
func (c *TxClient) SubmitTx(ctx context.Context, msgs []sdktypes.Msg, opts ...user.TxOption) (*sdktypes.TxResponse, error) {
	legacyResp, err := c.TxClient.SubmitTx(ctx, msgs, opts...)
	if err != nil {
		return nil, err
	}

	return c.buildSDKTxResponse(legacyResp), nil
}

// SubmitPayForBlobToQueue calls the original SubmitPayForBlobToQueue method and returns a v2 sdk.TxResponse.
func (c *TxClient) SubmitPayForBlobToQueue(ctx context.Context, blobs []*share.Blob, opts ...user.TxOption) (*sdktypes.TxResponse, error) {
	legacyResp, err := c.TxClient.SubmitPayForBlobToQueue(ctx, blobs, opts...)
	if err != nil {
		return nil, err
	}

	return c.buildSDKTxResponse(legacyResp), nil
}

// ConfirmTx calls the original ConfirmTx method and returns a v2 sdk.TxResponse.
func (c *TxClient) ConfirmTx(ctx context.Context, txHash string) (*sdktypes.TxResponse, error) {
	legacyResp, err := c.TxClient.ConfirmTx(ctx, txHash)
	if err != nil {
		return nil, err
	}

	return c.buildSDKTxResponse(legacyResp), nil
}

// StartSequentialQueue starts a sequential submission queue for the given account.
func (c *TxClient) StartSequentialQueue(ctx context.Context, accountName string) error {
	return c.StartSequentialQueueWithPollTime(ctx, accountName, user.DefaultPollTime)
}

// StartSequentialQueueWithPollTime starts a sequential queue with a custom poll time.
func (c *TxClient) StartSequentialQueueWithPollTime(ctx context.Context, accountName string, pollTime time.Duration) error {
	c.queueMu.Lock()
	defer c.queueMu.Unlock()

	if _, exists := c.sequentialQueues[accountName]; exists {
		return fmt.Errorf("sequential queue already running for account %s", accountName)
	}

	queue := newSequentialQueue(c, accountName, pollTime)
	if err := queue.start(ctx); err != nil {
		return fmt.Errorf("failed to start sequential queue: %w", err)
	}

	c.sequentialQueues[accountName] = queue
	return nil
}

// StopSequentialQueue stops the sequential queue for the given account.
func (c *TxClient) StopSequentialQueue(accountName string) {
	c.queueMu.Lock()
	defer c.queueMu.Unlock()

	if queue, exists := c.sequentialQueues[accountName]; exists {
		queue.stop()
		delete(c.sequentialQueues, accountName)
	}
}

// StopAllSequentialQueues stops all running sequential queues.
func (c *TxClient) StopAllSequentialQueues() {
	c.queueMu.Lock()
	defer c.queueMu.Unlock()

	for accountName, queue := range c.sequentialQueues {
		queue.stop()
		delete(c.sequentialQueues, accountName)
	}
}

// SubmitPFBToSequentialQueue submits blobs using the sequential queue for the default account.
func (c *TxClient) SubmitPFBToSequentialQueue(ctx context.Context, blobs []*share.Blob, opts ...user.TxOption) (*sdktypes.TxResponse, error) {
	return c.SubmitPFBToSequentialQueueWithAccount(ctx, c.DefaultAccountName(), blobs, opts...)
}

// SubmitPFBToSequentialQueueWithAccount submits blobs using the sequential queue for the specified account.
func (c *TxClient) SubmitPFBToSequentialQueueWithAccount(ctx context.Context, accountName string, blobs []*share.Blob, opts ...user.TxOption) (*sdktypes.TxResponse, error) {
	c.queueMu.RLock()
	queue, exists := c.sequentialQueues[accountName]
	c.queueMu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("sequential queue not started for account %s", accountName)
	}

	resultsC := make(chan SequentialSubmissionResult, 1)
	defer close(resultsC)

	job := &SequentialSubmissionJob{
		Blobs:    blobs,
		Options:  opts,
		Ctx:      ctx,
		ResultsC: resultsC,
	}

	queue.submitJob(job)

	// Wait for result
	select {
	case result := <-resultsC:
		if result.Error != nil {
			return nil, result.Error
		}
		return result.TxResponse, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// GetSequentialQueueSize returns the number of pending transactions in the queue for the given account.
func (c *TxClient) GetSequentialQueueSize(accountName string) (int, error) {
	c.queueMu.RLock()
	queue, exists := c.sequentialQueues[accountName]
	c.queueMu.RUnlock()

	if !exists {
		return 0, fmt.Errorf("sequential queue not started for account %s", accountName)
	}

	return queue.GetQueueSize(), nil
}

// BroadcastPayForBlobWithoutRetry broadcasts a PayForBlob transaction without automatic retry logic.
// TTODO: this will do for now, but i should refactor tx client functions without breaking the API to make it more usable
func (c *TxClient) BroadcastPayForBlobWithoutRetry(ctx context.Context, accountName string, blobs []*share.Blob, opts ...user.TxOption) (*sdktypes.TxResponse, error) {
	// Lock the client for the duration of build+sign+send
	c.TxClient.mtx.Lock()
	defer c.TxClient.mtx.Unlock()

	// Check account is loaded
	if err := c.TxClient.CheckAccountLoadedExported(ctx, accountName); err != nil {
		return nil, err
	}

	acc := c.Signer().Account(accountName)
	if acc == nil {
		return nil, fmt.Errorf("account %s not found", accountName)
	}

	// Build the transaction bytes
	txBytes, _, err := c.Signer().CreatePayForBlobs(accountName, blobs, opts...)
	if err != nil {
		return nil, err
	}

	// Get the sequence before sending
	sequence := c.Signer().Account(accountName).Sequence()

	// Send directly without retry logic
	conn := c.GetConn()
	if conn == nil {
		return nil, fmt.Errorf("no connection available")
	}

	resp, err := c.TxClient.SendTxToConnectionExported(ctx, conn, txBytes)
	if err != nil {
		return nil, err
	}

	// Track the transaction
	c.TxClient.GetTxTrackerExported().TrackTransaction(accountName, sequence, resp.TxHash, txBytes)

	// Increment sequence after successful broadcast
	if err := c.Signer().IncrementSequence(accountName); err != nil {
		return nil, fmt.Errorf("error incrementing sequence: %w", err)
	}

	return resp, nil
}
