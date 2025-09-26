package user

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"cosmossdk.io/x/feegrant"
	"github.com/celestiaorg/celestia-app/v6/pkg/appconsts"
	"github.com/celestiaorg/go-square/v3/share"
	"github.com/cosmos/cosmos-sdk/crypto/hd"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	sdktypes "github.com/cosmos/cosmos-sdk/types"
	bank "github.com/cosmos/cosmos-sdk/x/bank/types"
)

// SubmissionJob represents a transaction submission task for parallel processing
type SubmissionJob struct {
	ID       string
	Blobs    []*share.Blob
	Options  []TxOption
	ResultsC chan SubmissionResult
}

// SubmissionResult contains the result of a parallel transaction submission
type SubmissionResult struct {
	Signer     string
	TxResponse *TxResponse
	Error      error
}

// ParallelTxPool manages parallel transaction submission
type ParallelTxPool struct {
	mtx         sync.RWMutex
	client      *TxClient
	jobQueue    chan *SubmissionJob
	workers     []*TxWorker
	started     atomic.Bool
	stopCh      chan struct{}
	initialize  bool        // whether to initialize workers automatically
	initialized atomic.Bool // whether workers have been initialized
}

// TxWorker represents a worker that processes transactions using a specific account
type TxWorker struct {
	id          int
	accountName string
	address     string
	client      *TxClient
	jobQueue    chan *SubmissionJob
	stopCh      chan struct{}
}

const (
	defaultParallelQueueSize = 100
)

func newParallelTxPool(client *TxClient, numWorkers int, initialize bool) *ParallelTxPool {
	pool := &ParallelTxPool{
		client:     client,
		jobQueue:   make(chan *SubmissionJob, defaultParallelQueueSize),
		workers:    make([]*TxWorker, numWorkers),
		stopCh:     make(chan struct{}),
		initialize: initialize,
	}

	// Create workers: first worker always uses existing signer account
	for i := 0; i < numWorkers; i++ {
		var accountName, address string

		if i == 0 {
			// First worker uses the existing default account
			accountName = client.DefaultAccountName()
			address = client.DefaultAddress().String()
		} else {
			// Additional workers use generated account names
			accountName = fmt.Sprintf("parallel-worker-%d", i)

			// Get worker address from keyring if account exists
			if record, err := client.signer.keys.Key(accountName); err == nil {
				if addr, err := record.GetAddress(); err == nil {
					address = addr.String()
				}
			}
		}

		worker := &TxWorker{
			id:          i,
			accountName: accountName,
			address:     address,
			client:      client,
			jobQueue:    pool.jobQueue,
			stopCh:      make(chan struct{}),
		}
		pool.workers[i] = worker
	}

	return pool
}

// NewParallelTxPool creates a new parallel transaction submission pool.
func NewParallelTxPool(client *TxClient, numWorkers int, initialize bool) *ParallelTxPool {
	return newParallelTxPool(client, numWorkers, initialize)
}

// Start initiates all workers in the pool
func (p *ParallelTxPool) Start(ctx context.Context) error {
	p.mtx.Lock()
	defer p.mtx.Unlock()

	if p.started.Load() {
		return nil
	}

	// Initialize workers if configured to do so
	if p.initialize && !p.initialized.Load() {
		if err := p.client.InitializeWorkerAccounts(ctx); err != nil {
			return fmt.Errorf("failed to initialize worker accounts: %w", err)
		}
		p.initialized.Store(true)
	}

	for _, worker := range p.workers {
		go worker.Start()
	}

	p.started.Store(true)
	return nil
}

// Stop shuts down all workers in the pool
func (p *ParallelTxPool) Stop() {
	p.mtx.Lock()
	defer p.mtx.Unlock()

	if !p.started.Load() {
		return
	}

	close(p.stopCh)
	for _, worker := range p.workers {
		close(worker.stopCh)
	}
	p.started.Store(false)
}

// SubmitJob submits a job to the parallel worker pool
func (p *ParallelTxPool) SubmitJob(job *SubmissionJob) {
	select {
	case p.jobQueue <- job:
	case <-p.stopCh:
		job.ResultsC <- SubmissionResult{Error: errors.New("parallel pool full or has stopped")}
	}
}

// Workers returns the workers in the parallel pool
func (p *ParallelTxPool) Workers() []*TxWorker {
	return p.workers
}

// IsStarted returns whether the parallel pool is started
func (p *ParallelTxPool) IsStarted() bool {
	return p.started.Load()
}

// Start begins the worker's job processing loop
func (w *TxWorker) Start() {
	for {
		select {
		case job := <-w.jobQueue:
			w.processJob(job)
		case <-w.stopCh:
			return
		}
	}
}

// processJob handles the actual transaction submission for a job
func (w *TxWorker) processJob(job *SubmissionJob) {
	ctx := context.Background()

	// Add fee granter option so master account pays for worker transaction fees
	job.Options = append(job.Options, SetFeeGranter(w.client.DefaultAddress()))

	// Use the worker's dedicated account to submit the transaction
	txResponse, err := w.client.SubmitPayForBlobWithAccount(ctx, w.accountName, job.Blobs, job.Options...)

	result := SubmissionResult{
		Signer:     w.address,
		TxResponse: txResponse,
		Error:      err,
	}

	// Send result back through the job-specific results channel
	job.ResultsC <- result
}

// SubmitPayForBlobParallel submits a transaction for parallel processing and returns a channel for the result.
// Returns a channel that will receive the result and an error only if the parallel pool is not configured.
func (client *TxClient) SubmitPayForBlobParallel(ctx context.Context, blobs []*share.Blob, opts ...TxOption) (chan SubmissionResult, error) {
	if client.parallelPool == nil {
		return nil, errors.New("parallel submission not configured - use WithTxWorkers option")
	}

	// Initialize and start the pool on first use when auto-initialization is enabled.
	if !client.parallelPool.started.Load() {
		if client.parallelPool.initialize {
			if err := client.parallelPool.Start(ctx); err != nil {
				return nil, fmt.Errorf("failed to start parallel pool: %w", err)
			}
		} else {
			return nil, errors.New("parallel pool not started - call ParallelPool().Start() first")
		}
	}

	jobID := fmt.Sprintf("job_%d", time.Now().UnixNano())
	resultsC := make(chan SubmissionResult, 1)

	job := &SubmissionJob{
		ID:       jobID,
		Blobs:    blobs,
		Options:  opts,
		ResultsC: resultsC,
	}

	client.parallelPool.SubmitJob(job)

	return resultsC, nil
}

// InitializeWorkerAccounts creates and initializes all worker accounts for parallel submission.
// It creates the accounts in the keyring if they don't exist, funds them with a small balance,
// and sets up fee grants so the main account pays for transaction fees.
// This method should be called after TxClient creation but before parallel submissions.
func (client *TxClient) InitializeWorkerAccounts(ctx context.Context) error {
	if client.parallelPool == nil {
		return errors.New("parallel pool not configured - use WithTxWorkers option")
	}

	// No work required if we've already initialized all workers.
	if client.parallelPool.initialized.Load() {
		return nil
	}

	// Get the list of worker accounts that need to be initialized
	// Skip the first worker (index 0) as it always uses the existing signer account
	var workersToInit []*TxWorker
	for i, worker := range client.parallelPool.workers {
		if i == 0 {
			// Skip first worker - it uses existing account
			continue
		}
		// Check if account exists in signer
		if _, exists := client.signer.accounts[worker.accountName]; !exists {
			workersToInit = append(workersToInit, worker)
		}
	}

	if len(workersToInit) == 0 {
		client.parallelPool.initialized.Store(true)
		return nil // All accounts already exist
	}

	// Create accounts in keyring if they don't exist
	for _, worker := range workersToInit {
		if err := client.createWorkerAccount(worker.accountName); err != nil {
			return fmt.Errorf("failed to create worker account %s: %w", worker.accountName, err)
		}
	}

	// Fund accounts and set up fee grants in a single transaction
	if err := client.fundAndGrantWorkerAccounts(ctx, workersToInit); err != nil {
		return fmt.Errorf("failed to fund and grant worker accounts: %w", err)
	}

	return nil
}

// createWorkerAccount creates a new account in the keyring
func (client *TxClient) createWorkerAccount(accountName string) error {
	// Check if account already exists in keyring
	if _, err := client.signer.keys.Key(accountName); err == nil {
		return nil // Account already exists
	}

	// Create new account
	path := hd.CreateHDPath(sdktypes.CoinType, 0, 0).String()
	_, _, err := client.signer.keys.NewMnemonic(accountName, keyring.English, path, keyring.DefaultBIP39Passphrase, hd.Secp256k1)
	if err != nil {
		return fmt.Errorf("failed to create account %s in keyring: %w", accountName, err)
	}

	return nil
}

// fundAndGrantWorkerAccounts sends funds to worker accounts and sets up fee grants
func (client *TxClient) fundAndGrantWorkerAccounts(ctx context.Context, workers []*TxWorker) error {
	if len(workers) == 0 {
		return nil
	}

	msgs := make([]sdktypes.Msg, 0, len(workers)*2) // Each worker needs 2 msgs: send + feegrant
	totalGasLimit := uint64(0)

	masterAddress := client.defaultAddress

	for _, worker := range workers {
		// Get worker address
		record, err := client.signer.keys.Key(worker.accountName)
		if err != nil {
			return fmt.Errorf("failed to get worker account %s from keyring: %w", worker.accountName, err)
		}

		workerAddress, err := record.GetAddress()
		if err != nil {
			return fmt.Errorf("failed to get address for worker account %s: %w", worker.accountName, err)
		}

		// Create send message to fund the account
		sendMsg := bank.NewMsgSend(
			masterAddress,
			workerAddress,
			sdktypes.NewCoins(sdktypes.NewInt64Coin(appconsts.BondDenom, DefaultWorkerBalance)),
		)
		msgs = append(msgs, sendMsg)
		totalGasLimit += SendGasLimit

		// Create feegrant message so master account pays for worker fees
		feegrantMsg, err := feegrant.NewMsgGrantAllowance(
			&feegrant.BasicAllowance{}, // Unlimited allowance
			masterAddress,
			workerAddress,
		)
		if err != nil {
			return fmt.Errorf("failed to create feegrant message for worker %s: %w", worker.accountName, err)
		}
		msgs = append(msgs, feegrantMsg)
		totalGasLimit += FeegrantGasLimit
	}

	// Submit the initialization transaction
	opts := []TxOption{SetGasLimit(totalGasLimit)}
	_, err := client.SubmitTx(ctx, msgs, opts...)
	if err != nil {
		return fmt.Errorf("failed to submit initialization transaction: %w", err)
	}

	// Add the worker accounts to the signer
	for _, worker := range workers {
		record, err := client.signer.keys.Key(worker.accountName)
		if err != nil {
			return fmt.Errorf("failed to get worker account %s from keyring: %w", worker.accountName, err)
		}

		workerAddress, err := record.GetAddress()
		if err != nil {
			return fmt.Errorf("failed to get address for worker account %s: %w", worker.accountName, err)
		}

		// Query the account info from chain
		accNum, seqNum, err := QueryAccount(ctx, client.conns[0], client.registry, workerAddress)
		if err != nil {
			return fmt.Errorf("failed to query worker account %s on chain: %w", worker.accountName, err)
		}

		// Add account to signer
		account := NewAccount(worker.accountName, accNum, seqNum)
		if err := client.signer.AddAccount(account); err != nil {
			return fmt.Errorf("failed to add worker account %s to signer: %w", worker.accountName, err)
		}

		// Update worker address now that account is created
		worker.address = workerAddress.String()
	}

	client.parallelPool.initialized.Store(true)
	return nil
}
