package user

import (
	"context"
	"errors"
	"fmt"
	"sync"

	sdkmath "cosmossdk.io/math"
	"cosmossdk.io/x/feegrant"
	"github.com/celestiaorg/celestia-app/v6/pkg/appconsts"
	"github.com/celestiaorg/go-square/v2/share"
	"github.com/cosmos/cosmos-sdk/crypto/hd"
	sdktypes "github.com/cosmos/cosmos-sdk/types"
	bank "github.com/cosmos/cosmos-sdk/x/bank/types"
)

// txJob represents a tx to be submitted by a worker
type txJob struct {
	id     int64
	blobs  []*share.Blob
	opts   []TxOption
	result chan *TxResponse
	errCh  chan error
}

// txWorker represents a worker that processes PayForBlob jobs using a dedicated service account
type txWorker struct {
	id          int
	accountName string
	client      *TxClient
	jobCh       chan *txJob
	stopCh      chan struct{}
	wg          *sync.WaitGroup
}

// UnorderedTxPool manages parallel transaction submission using multiple service accounts
type UnorderedTxPool struct {
	client        *TxClient
	masterAccount string
	workers       []*txWorker
	jobQueue      chan *txJob
	nextJobID     int64
	mtx           sync.Mutex
	isRunning     bool
	stopCh        chan struct{}
	wg            sync.WaitGroup
}

// worker run method
func (w *txWorker) run() {
	defer w.wg.Done()

	for {
		select {
		case job := <-w.jobCh:
			// Process the job using the dedicated service account
			resp, err := w.client.SubmitPayForBlobWithAccount(context.Background(), w.accountName, job.blobs, job.opts...)
			if err != nil {
				select {
				case job.errCh <- err:
				default:
				}
			} else {
				select {
				case job.result <- resp:
				default:
				}
			}
		case <-w.stopCh:
			return
		}
	}
}

// InitServiceAccounts creates and funds service accounts for parallel transaction submission
// It creates numAccounts service accounts and sets up fee granter relationships
func (client *TxClient) InitServiceAccounts(ctx context.Context, masterAccount string, numAccounts int) error {
	client.mtx.Lock()
	defer client.mtx.Unlock()

	if numAccounts <= 0 {
		return errors.New("number of accounts must be positive")
	}

	// Check if master account exists
	if _, exists := client.signer.accounts[masterAccount]; !exists {
		return fmt.Errorf("master account %s not found", masterAccount)
	}

	var newAccounts []string
	var existingAccounts []string

	// Check which service accounts need to be created
	for i := 0; i < numAccounts; i++ {
		serviceAccountName := fmt.Sprintf("service-account-%d", i)

		// Check if service account already exists in keyring
		_, err := client.signer.keys.Key(serviceAccountName)
		if err != nil {
			newAccounts = append(newAccounts, serviceAccountName)
		} else {
			existingAccounts = append(existingAccounts, serviceAccountName)
		}
	}

	// Load existing accounts into signer
	for _, accountName := range existingAccounts {
		if err := client.checkAccountLoaded(ctx, accountName); err != nil {
			return fmt.Errorf("failed to load existing service account %s: %w", accountName, err)
		}
	}

	// Create and fund new accounts in batches
	if len(newAccounts) > 0 {
		if err := client.batchCreateAndFundAccounts(ctx, masterAccount, newAccounts); err != nil {
			return fmt.Errorf("failed to create and fund service accounts: %w", err)
		}
	}

	// Setup fee granter relationships in batch
	allAccounts := append(existingAccounts, newAccounts...)
	if err := client.batchSetupFeeGranter(ctx, masterAccount, allAccounts); err != nil {
		return fmt.Errorf("failed to setup fee granters: %w", err)
	}

	return nil
}

// batchCreateAndFundAccounts creates multiple service accounts and funds them in a single transaction
func (client *TxClient) batchCreateAndFundAccounts(ctx context.Context, masterAccount string, accountNames []string) error {
	if len(accountNames) == 0 {
		return nil
	}

	// Check if master account exists
	masterAcc, exists := client.signer.accounts[masterAccount]
	if !exists {
		return fmt.Errorf("master account %s not found", masterAccount)
	}

	var fundingMsgs []sdktypes.Msg
	masterAddr := masterAcc.Address()
	fundingAmount := sdktypes.NewCoins(sdktypes.NewCoin(appconsts.BondDenom, sdkmath.NewInt(1)))

	for _, accountName := range accountNames {
		record, err := client.signer.keys.NewAccount(accountName, "", "", "", hd.Secp256k1)
		if err != nil {
			return fmt.Errorf("failed to create key for %s: %w", accountName, err)
		}

		addr, err := record.GetAddress()
		if err != nil {
			return fmt.Errorf("failed to get address for %s: %w", accountName, err)
		}

		sendMsg := &bank.MsgSend{
			FromAddress: masterAddr.String(),
			ToAddress:   addr.String(),
			Amount:      fundingAmount,
		}
		fundingMsgs = append(fundingMsgs, sendMsg)
	}

	_, err := client.BroadcastTx(ctx, fundingMsgs)
	if err != nil {
		return fmt.Errorf("failed to fund service accounts: %w", err)
	}

	for _, accountName := range accountNames {
		if err := client.checkAccountLoaded(ctx, accountName); err != nil {
			return fmt.Errorf("failed to load service account %s: %w", accountName, err)
		}
	}

	return nil
}

// batchSetupFeeGranter sets up fee granter relationships for multiple accounts in a single transaction
func (client *TxClient) batchSetupFeeGranter(ctx context.Context, masterAccount string, serviceAccounts []string) error {
	if len(serviceAccounts) == 0 {
		return nil
	}

	masterAcc, exists := client.signer.accounts[masterAccount]
	if !exists {
		return fmt.Errorf("master account %s not found", masterAccount)
	}

	masterAddr := masterAcc.Address()
	var grantMsgs []sdktypes.Msg

	for _, serviceAccount := range serviceAccounts {
		serviceAcc, exists := client.signer.accounts[serviceAccount]
		if !exists {
			return fmt.Errorf("service account %s not found", serviceAccount)
		}

		serviceAddr := serviceAcc.Address()

		allowance := &feegrant.BasicAllowance{}

		grantMsg, err := feegrant.NewMsgGrantAllowance(allowance, masterAddr, serviceAddr)
		if err != nil {
			return fmt.Errorf("failed to create grant allowance message for %s: %w", serviceAccount, err)
		}

		grantMsgs = append(grantMsgs, grantMsg)
	}

	_, err := client.SubmitTx(ctx, grantMsgs)
	if err != nil {
		return fmt.Errorf("failed to setup fee granters: %w", err)
	}

	return nil
}

// InitUnorderedTxPool initializes the unordered transaction pool with service accounts
func (client *TxClient) InitUnorderedTxPool(ctx context.Context, masterAccount string, numWorkers int) error {
	client.mtx.Lock()
	defer client.mtx.Unlock()

	if client.unorderedPool != nil && client.unorderedPool.isRunning {
		return errors.New("unordered tx pool is already running")
	}

	// Initialize service accounts
	if err := client.InitServiceAccounts(ctx, masterAccount, numWorkers); err != nil {
		return fmt.Errorf("failed to initialize service accounts: %w", err)
	}

	// Create unordered pool
	pool := &UnorderedTxPool{
		client:        client,
		masterAccount: masterAccount,
		workers:       make([]*txWorker, numWorkers),
		jobQueue:      make(chan *txJob, numWorkers*10), // Buffer for jobs
		stopCh:        make(chan struct{}),
	}

	// Create workers
	for i := 0; i < numWorkers; i++ {
		serviceAccountName := fmt.Sprintf("service-account-%d", i)
		worker := &txWorker{
			id:          i,
			accountName: serviceAccountName,
			client:      client,
			jobCh:       pool.jobQueue,
			stopCh:      pool.stopCh,
			wg:          &pool.wg,
		}
		pool.workers[i] = worker
	}

	client.unorderedPool = pool
	return nil
}

// StartUnorderedTxPool starts the worker pool for processing unordered transactions
func (client *TxClient) StartUnorderedTxPool() error {
	client.mtx.Lock()
	defer client.mtx.Unlock()

	if client.unorderedPool == nil {
		return errors.New("unordered tx pool not initialized")
	}

	if client.unorderedPool.isRunning {
		return errors.New("unordered tx pool is already running")
	}

	client.unorderedPool.isRunning = true

	// Start all workers
	for _, worker := range client.unorderedPool.workers {
		client.unorderedPool.wg.Add(1)
		go worker.run()
	}

	return nil
}

// StopUnorderedTxPool stops the worker pool
func (client *TxClient) StopUnorderedTxPool() error {
	client.mtx.Lock()
	defer client.mtx.Unlock()

	if client.unorderedPool == nil || !client.unorderedPool.isRunning {
		return nil
	}

	close(client.unorderedPool.stopCh)
	client.unorderedPool.wg.Wait()
	client.unorderedPool.isRunning = false

	return nil
}

// SubmitPayForBlobUnordered submits PayForBlob transactions using the unordered worker pool
// This method provides parallel transaction submission without sequence number conflicts.
func (client *TxClient) SubmitPayForBlobUnordered(ctx context.Context, blobs []*share.Blob, opts ...TxOption) (*TxResponse, error) {
	client.mtx.Lock()
	pool := client.unorderedPool
	client.mtx.Unlock()

	if pool == nil {
		return nil, errors.New("unordered tx pool not initialized")
	}

	if !pool.isRunning {
		return nil, errors.New("unordered tx pool not running")
	}

	pool.mtx.Lock()
	pool.nextJobID++
	jobID := pool.nextJobID
	pool.mtx.Unlock()

	job := &txJob{
		id:     jobID,
		blobs:  blobs,
		opts:   opts,
		result: make(chan *TxResponse, 1),
		errCh:  make(chan error, 1),
	}

	select {
	case pool.jobQueue <- job:
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	select {
	case result := <-job.result:
		return result, nil
	case err := <-job.errCh:
		return nil, err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// GetUnorderedPoolStats returns statistics about the unordered transaction pool
func (client *TxClient) GetUnorderedPoolStats() (numWorkers int, queueSize int, isRunning bool) {
	client.mtx.Lock()
	defer client.mtx.Unlock()

	if client.unorderedPool == nil {
		return 0, 0, false
	}

	return len(client.unorderedPool.workers), len(client.unorderedPool.jobQueue), client.unorderedPool.isRunning
}
