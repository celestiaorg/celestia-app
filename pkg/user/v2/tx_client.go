package v2

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/celestiaorg/celestia-app/v6/app/encoding"
	apperrors "github.com/celestiaorg/celestia-app/v6/app/errors"
	"github.com/celestiaorg/celestia-app/v6/pkg/user"
	"github.com/celestiaorg/go-square/v3/share"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	sdktypes "github.com/cosmos/cosmos-sdk/types"
	sdktx "github.com/cosmos/cosmos-sdk/types/tx"
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

	// Primary GRPC connection (stored separately for access)
	conn *grpc.ClientConn
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
		conn:             conn,
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
		conn:             conn,
	}
	if err := v2Client.StartSequentialQueue(ctx, v1Client.DefaultAccountName()); err != nil {
		return nil, err
	}
	return v2Client, nil
}

// Wrapv1TxClient wraps a v1 TxClient and returns a v2 TxClient.
// Note: connection will be nil, so sequential queue features requiring direct connection access won't work
func Wrapv1TxClient(v1Client *user.TxClient) *TxClient {
	return &TxClient{
		TxClient:         v1Client,
		sequentialQueues: make(map[string]*sequentialQueue),
		conn:             nil,
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
	queue.start();

	c.sequentialQueues[accountName] = queue
	return nil
}

// StopSequentialQueue stops the sequential queue for the given account.
func (c *TxClient) StopSequentialQueue(accountName string) {
	c.queueMu.Lock()
	defer c.queueMu.Unlock()

	if queue, exists := c.sequentialQueues[accountName]; exists {
		queue.cancel()
		delete(c.sequentialQueues, accountName)
	}
}

// StopAllSequentialQueues stops all running sequential queues.
func (c *TxClient) StopAllSequentialQueues() {
	c.queueMu.Lock()
	defer c.queueMu.Unlock()

	for accountName, queue := range c.sequentialQueues {
		queue.cancel()
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
	// Note: We don't close this channel because the queue worker goroutines
	// may still send on it after ctx.Done(). The channel will be GC'd naturally.

	job := &SequentialSubmissionJob{
		Blobs:    blobs,
		Options:  opts,
		Ctx:      ctx,
		ResultsC: resultsC,
	}
	fmt.Println("Submitting job to sequential queue")
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
func (c *TxClient) BroadcastPayForBlobWithoutRetry(ctx context.Context, accountName string, blobs []*share.Blob, opts ...user.TxOption) (*sdktypes.TxResponse, error) {
	// Use BroadcastPayForBlobWithAccount but without confirmation
	resp, err := c.TxClient.BroadcastPayForBlobWithAccount(ctx, accountName, blobs, opts...)
	if err != nil {
		return nil, err
	}

	return &sdktypes.TxResponse{
		Height:    resp.Height,
		TxHash:    resp.TxHash,
		Code:      resp.Code,
		Codespace: resp.Codespace,
		GasWanted: resp.GasWanted,
		GasUsed:   resp.GasUsed,
		Signers:   resp.Signers,
	}, nil
}

// ResubmitTxBytes resubmits a transaction using pre-signed bytes without retry logic
func (c *TxClient) ResubmitTxBytes(ctx context.Context, txBytes []byte) (*sdktypes.TxResponse, error) {
	// Get the connection for broadcasting
	conn := c.GetGRPCConnection()
	if conn == nil {
		return nil, fmt.Errorf("no connection available")
	}

	// Use the SDK tx service client to broadcast
	sdktxClient := sdktx.NewServiceClient(conn)
	resp, err := sdktxClient.BroadcastTx(ctx, &sdktx.BroadcastTxRequest{
		Mode:    sdktx.BroadcastMode_BROADCAST_MODE_SYNC,
		TxBytes: txBytes,
	})
	if err != nil {
		return nil, err
	}

	// Check if broadcast was successful
	if resp.TxResponse.Code != 0 {
		return nil, fmt.Errorf("broadcast failed with code %d: %s", resp.TxResponse.Code, resp.TxResponse.RawLog)
	}

	return resp.TxResponse, nil
}

// GetGRPCConnection returns the primary GRPC connection for creating tx status clients
func (c *TxClient) GetGRPCConnection() *grpc.ClientConn {
	return c.conn
}

// IsSequenceMismatchError checks if an error is a sequence mismatch (nonce mismatch)
func IsSequenceMismatchError(err error) bool {
	if err == nil {
		return false
	}

	// Check if it's a BroadcastTxError with sequence mismatch code
	broadcastTxErr, ok := err.(*user.BroadcastTxError)
	if !ok {
		return false
	}

	return apperrors.IsNonceMismatchCode(broadcastTxErr.Code)
}
