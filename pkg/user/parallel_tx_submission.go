package user

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

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
	Blobs    []*share.Blob
	Options  []TxOption
	Ctx      context.Context
	ResultsC chan SubmissionResult
}

// SubmissionResult contains the result of a parallel transaction submission
type SubmissionResult struct {
	Signer     string
	TxResponse *TxResponse
	Error      error
}

// txQueue manages parallel transaction submission
type txQueue struct {
	client      *TxClient
	jobQueue    chan *SubmissionJob
	workers     []*txWorker
	started     atomic.Bool
	ctx         context.Context
	cancel      context.CancelFunc
	initialized atomic.Bool // whether workers have been initialized
	wg          sync.WaitGroup
}

// txWorker represents a worker that processes transactions using a specific account
type txWorker struct {
	id          int
	accountName string
	address     string
	client      *TxClient
	jobQueue    chan *SubmissionJob
}

const (
	defaultParallelQueueSize = 100
)

func newTxQueue(client *TxClient, numWorkers int) *txQueue {
	pool := &txQueue{
		client:   client,
		jobQueue: make(chan *SubmissionJob, defaultParallelQueueSize),
		workers:  make([]*txWorker, numWorkers),
	}

	// Create workers: first worker always uses existing signer account
	for i := range numWorkers {
		var accountName, address string

		if i == 0 {
			// First worker uses the current default account
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

		worker := &txWorker{
			id:          i,
			accountName: accountName,
			address:     address,
			client:      client,
			jobQueue:    pool.jobQueue,
		}
		pool.workers[i] = worker
	}

	return pool
}

// start initiates all workers in the pool
func (p *txQueue) start(ctx context.Context) error {
	if p.started.Load() {
		return nil
	}

	// Initialize worker accounts if needed
	if !p.initialized.Load() {
		if err := p.initializeWorkerAccounts(ctx); err != nil {
			return fmt.Errorf("failed to initialize worker accounts: %w", err)
		}
		p.initialized.Store(true)
	}

	// Recreate job queue channel if it was closed during previous stop
	p.jobQueue = make(chan *SubmissionJob, defaultParallelQueueSize)
	// Update workers to use new job queue BEFORE starting goroutines
	for _, worker := range p.workers {
		worker.jobQueue = p.jobQueue
	}

	// Create a new context for this pool instance
	p.ctx, p.cancel = context.WithCancel(ctx)

	// Set started flag before starting workers to prevent race
	p.started.Store(true)

	// Start workers after everything is set up
	for _, worker := range p.workers {
		p.wg.Add(1)
		go func(w *txWorker) {
			defer p.wg.Done()
			w.start(p.ctx)
		}(worker)
	}

	return nil
}

// stop shuts down all workers in the pool
func (p *txQueue) stop() {
	if !p.started.Load() {
		return
	}

	if p.cancel != nil {
		p.cancel()
	}

	// Close the job queue to signal workers to stop accepting new jobs
	close(p.jobQueue)

	// Wait for all workers to finish before marking as stopped
	p.wg.Wait()

	p.started.Store(false)
}

// submitJob submits a job to the parallel worker pool
func (p *txQueue) submitJob(job *SubmissionJob) {
	if !p.started.Load() || p.ctx == nil {
		job.ResultsC <- SubmissionResult{Error: errors.New("tx queue not started")}
		return
	}

	select {
	case p.jobQueue <- job:
	case <-p.ctx.Done():
		job.ResultsC <- SubmissionResult{Error: errors.New("tx queue full or has stopped")}
	}
}

// isStarted returns whether the tx queue is started
func (p *txQueue) isStarted() bool {
	return p.started.Load()
}

// start begins the worker's job processing loop
func (w *txWorker) start(ctx context.Context) {
	for {
		select {
		case job, ok := <-w.jobQueue:
			if !ok {
				// Channel closed, exit worker
				return
			}
			w.processJob(job, ctx)
		case <-ctx.Done():
			return
		}
	}
}

// processJob handles the actual transaction submission for a job
func (w *txWorker) processJob(job *SubmissionJob, workerCtx context.Context) {
	jobCtx := job.Ctx
	if jobCtx == nil {
		jobCtx = context.Background()
	}

	select {
	case <-jobCtx.Done():
		job.ResultsC <- SubmissionResult{Signer: w.address, Error: jobCtx.Err()}
		return
	case <-workerCtx.Done():
		job.ResultsC <- SubmissionResult{Signer: w.address, Error: workerCtx.Err()}
		return
	default:
	}

	options := job.Options

	// Only add fee granter option for workers that aren't the primary account (worker 0)
	if w.id != 0 {
		// Add fee granter option so master account pays for worker transaction fees
		options = append([]TxOption{SetFeeGranter(w.client.DefaultAddress())}, options...)
	}

	// Use the worker's dedicated account to submit the transaction
	txResponse, err := w.client.SubmitPayForBlobWithAccount(jobCtx, w.accountName, job.Blobs, options...)

	result := SubmissionResult{
		Signer:     w.address,
		TxResponse: txResponse,
		Error:      err,
	}

	// Send result back through the job-specific results channel
	job.ResultsC <- result
}

// initializeWorkerAccounts creates and initializes all worker accounts for parallel submission.
// It creates the accounts in the keyring if they don't exist, funds them with a small balance,
// and sets up fee grants so the main account pays for transaction fees.
func (p *txQueue) initializeWorkerAccounts(ctx context.Context) error {
	// No work required if we've already initialized all workers.
	if p.initialized.Load() {
		return nil
	}

	// If we only have one worker (index 0), skip all initialization as it uses the existing signer account
	if len(p.workers) == 1 {
		p.initialized.Store(true)
		return nil
	}

	// Get the list of worker accounts that need to be initialized
	// Skip the first worker (index 0) as it always uses the existing signer account
	var workersToInit []*txWorker
	var workersToLoad []*txWorker
	for i, worker := range p.workers {
		if i == 0 {
			// Skip first worker - it uses existing account
			continue
		}

		// Check if account exists in signer
		if _, exists := p.client.signer.accounts[worker.accountName]; !exists {
			// Check if account exists in keyring but not loaded in signer
			if _, err := p.client.signer.keys.Key(worker.accountName); err == nil {
				// Account exists in keyring but not loaded - add to load list
				workersToLoad = append(workersToLoad, worker)
			} else {
				// Account doesn't exist anywhere - needs full initialization
				workersToInit = append(workersToInit, worker)
			}
		}
	}

	// Load existing accounts from keyring into signer
	if len(workersToLoad) > 0 {
		for _, worker := range workersToLoad {
			if err := p.client.loadWorkerAccount(worker); err != nil {
				return fmt.Errorf("failed to load existing worker account %s: %w", worker.accountName, err)
			}
		}
	}

	if len(workersToInit) == 0 {
		p.initialized.Store(true)
		return nil // All accounts already exist
	}

	// Create accounts in keyring if they don't exist
	for _, worker := range workersToInit {
		if err := p.client.createWorkerAccount(worker.accountName); err != nil {
			return fmt.Errorf("failed to create worker account %s: %w", worker.accountName, err)
		}
	}

	// Fund accounts and set up fee grants in a single transaction
	if err := p.client.fundAndGrantWorkerAccounts(ctx, workersToInit); err != nil {
		return fmt.Errorf("failed to fund and grant worker accounts: %w", err)
	}

	return nil
}

// createFeeGrantMessages creates fee grant messages for workers that don't already have grants
func (client *TxClient) createFeeGrantMessages(ctx context.Context, workers []*txWorker) ([]sdktypes.Msg, uint64, error) {
	msgs := make([]sdktypes.Msg, 0, len(workers))
	totalGasLimit := uint64(0)
	masterAddress := client.defaultAddress

	for _, worker := range workers {
		// Get worker address
		record, err := client.signer.keys.Key(worker.accountName)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to get worker account %s from keyring: %w", worker.accountName, err)
		}

		workerAddress, err := record.GetAddress()
		if err != nil {
			return nil, 0, fmt.Errorf("failed to get address for worker account %s: %w", worker.accountName, err)
		}

		// Check if fee grant already exists
		hasGrant, err := client.hasFeeGrant(ctx, masterAddress, workerAddress)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to check fee grant for worker %s: %w", worker.accountName, err)
		}

		if !hasGrant {
			// Create feegrant message so master account pays for worker fees
			feegrantMsg, err := feegrant.NewMsgGrantAllowance(
				&feegrant.BasicAllowance{}, // Unlimited allowance
				masterAddress,
				workerAddress,
			)
			if err != nil {
				return nil, 0, fmt.Errorf("failed to create feegrant message for worker %s: %w", worker.accountName, err)
			}
			msgs = append(msgs, feegrantMsg)
			totalGasLimit += FeegrantGasLimit
		}
	}

	return msgs, totalGasLimit, nil
}

// loadWorkerAccount loads an existing account from keyring into the signer
func (client *TxClient) loadWorkerAccount(worker *txWorker) error {
	// Get account from keyring
	record, err := client.signer.keys.Key(worker.accountName)
	if err != nil {
		return fmt.Errorf("failed to get worker account %s from keyring: %w", worker.accountName, err)
	}

	workerAddress, err := record.GetAddress()
	if err != nil {
		return fmt.Errorf("failed to get address for worker account %s: %w", worker.accountName, err)
	}

	// Query account info from chain
	accNum, seqNum, err := QueryAccount(context.Background(), client.conns[0], client.registry, workerAddress)
	if err != nil {
		return fmt.Errorf("failed to query worker account %s on chain: %w", worker.accountName, err)
	}

	// Add account to signer
	account := NewAccount(worker.accountName, accNum, seqNum)
	if err := client.signer.AddAccount(account); err != nil {
		return fmt.Errorf("failed to add worker account %s to signer: %w", worker.accountName, err)
	}

	// Update worker address
	worker.address = workerAddress.String()

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

// accountNeedsFunding checks if an account needs funding by querying its balance
func (client *TxClient) accountNeedsFunding(ctx context.Context, address sdktypes.AccAddress) (bool, error) {
	// Query account balance
	balance, err := QueryAccountBalance(ctx, client.conns[0], client.registry, address, appconsts.BondDenom)
	if err != nil {
		// If account doesn't exist, it needs funding
		return true, nil
	}

	// Check if balance is less than the default worker balance
	// Note: we check for >= DefaultWorkerBalance to avoid re-funding accounts that already have sufficient balance
	return balance.Amount.Int64() < DefaultWorkerBalance, nil
}

// hasFeeGrant checks if a fee grant exists between granter and grantee
func (client *TxClient) hasFeeGrant(ctx context.Context, granter, grantee sdktypes.AccAddress) (bool, error) {
	feegrantQuery := feegrant.NewQueryClient(client.conns[0])
	_, err := feegrantQuery.Allowance(ctx, &feegrant.QueryAllowanceRequest{
		Granter: granter.String(),
		Grantee: grantee.String(),
	})
	if err != nil {
		// If error contains "not found" or similar, grant doesn't exist
		return false, nil
	}
	return true, nil
}

// fundAndGrantWorkerAccounts sends funds to worker accounts and sets up fee grants
func (client *TxClient) fundAndGrantWorkerAccounts(ctx context.Context, workers []*txWorker) error {
	if len(workers) == 0 {
		return nil
	}

	msgs := make([]sdktypes.Msg, 0, len(workers)*2) // Each worker needs up to 2 msgs: send + feegrant
	totalGasLimit := uint64(0)

	masterAddress := client.defaultAddress

	// Create send messages for funding accounts that need funding
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

		// Check if account already has sufficient balance
		needsFunding, err := client.accountNeedsFunding(ctx, workerAddress)
		if err != nil {
			// If we can't check balance, assume it needs funding to avoid blocking
			needsFunding = true
		}

		if needsFunding {
			// Create send message to fund the account
			sendMsg := bank.NewMsgSend(
				masterAddress,
				workerAddress,
				sdktypes.NewCoins(sdktypes.NewInt64Coin(appconsts.BondDenom, DefaultWorkerBalance)),
			)
			msgs = append(msgs, sendMsg)
			totalGasLimit += SendGasLimit
		}
	}

	// Add fee grant messages
	feeGrantMsgs, feeGrantGasLimit, err := client.createFeeGrantMessages(ctx, workers)
	if err != nil {
		return err
	}
	msgs = append(msgs, feeGrantMsgs...)
	totalGasLimit += feeGrantGasLimit

	// Submit the initialization transaction only if there are messages to send
	if len(msgs) > 0 {
		opts := []TxOption{SetGasLimit(totalGasLimit)}
		_, err = client.SubmitTx(ctx, msgs, opts...)
		if err != nil {
			return fmt.Errorf("failed to submit initialization transaction: %w", err)
		}
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

	client.txQueue.initialized.Store(true)
	return nil
}
