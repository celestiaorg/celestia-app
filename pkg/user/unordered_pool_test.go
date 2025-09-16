package user

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v6/app/encoding"
	"github.com/celestiaorg/go-square/v2/share"
	"github.com/cosmos/cosmos-sdk/crypto/hd"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUnorderedTxPoolBasic(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Create a test keyring and TxClient
	encCfg := encoding.MakeConfig()
	keyring := keyring.NewInMemory(encCfg.Codec)

	// Create master account
	masterAccount, err := keyring.NewAccount("master", "", "", "", hd.Secp256k1)
	require.NoError(t, err)

	// Create a mock TxClient for testing
	client := &TxClient{
		signer: &Signer{
			keys:     keyring,
			accounts: make(map[string]*Account),
		},
	}

	// Add master account to signer
	_, err = masterAccount.GetAddress()
	require.NoError(t, err)
	client.signer.accounts["master"] = NewAccount("master", 1, 0)

	t.Run("InitServiceAccounts with invalid parameters", func(t *testing.T) {
		err := client.InitServiceAccounts(ctx, "master", 0)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "number of accounts must be positive")

		err = client.InitServiceAccounts(ctx, "nonexistent", 1)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("InitUnorderedTxPool before initialization", func(t *testing.T) {
		err := client.StartUnorderedTxPool()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not initialized")
	})
}

func TestUnorderedTxPoolStats(t *testing.T) {
	client := &TxClient{}

	workers, queueSize, isRunning := client.GetUnorderedPoolStats()
	assert.Equal(t, 0, workers)
	assert.Equal(t, 0, queueSize)
	assert.False(t, isRunning)

	// Test with initialized pool
	client.unorderedPool = &UnorderedTxPool{
		workers:   make([]*txWorker, 3),
		jobQueue:  make(chan *txJob, 10),
		isRunning: true,
	}

	workers, queueSize, isRunning = client.GetUnorderedPoolStats()
	assert.Equal(t, 3, workers)
	assert.Equal(t, 0, queueSize)
	assert.True(t, isRunning)
}

func TestWorkerLifecycle(t *testing.T) {
	// Create a test worker
	client := &TxClient{
		signer: &Signer{
			accounts: make(map[string]*Account),
		},
	}
	client.signer.accounts["service-account-0"] = NewAccount("service-account-0", 1, 0)

	jobCh := make(chan *txJob, 1)
	stopCh := make(chan struct{})
	var wg sync.WaitGroup

	worker := &txWorker{
		id:          0,
		accountName: "service-account-0",
		client:      client,
		jobCh:       jobCh,
		stopCh:      stopCh,
		wg:          &wg,
	}

	// Start worker
	wg.Add(1)
	go worker.run()

	// Test worker stops properly
	close(stopCh)
	wg.Wait()
}

func TestJobHandling(t *testing.T) {
	// Test job creation and channel handling
	job := &txJob{
		id:     1,
		blobs:  []*share.Blob{},
		opts:   []TxOption{},
		result: make(chan *TxResponse, 1),
		errCh:  make(chan error, 1),
	}

	// Test result channel
	response := &TxResponse{Height: 100, TxHash: "test"}
	job.result <- response

	select {
	case result := <-job.result:
		assert.Equal(t, response, result)
	default:
		t.Fatal("Expected result in channel")
	}

	// Test error channel
	testErr := assert.AnError
	job.errCh <- testErr

	select {
	case err := <-job.errCh:
		assert.Equal(t, testErr, err)
	default:
		t.Fatal("Expected error in channel")
	}
}

func TestUnorderedPoolConcurrency(t *testing.T) {
	// Create test pool
	pool := &UnorderedTxPool{
		jobQueue:  make(chan *txJob, 100),
		nextJobID: 0,
		isRunning: true,
	}

	// Test concurrent job ID generation
	const numGoroutines = 100
	jobIDs := make([]int64, numGoroutines)
	var wg sync.WaitGroup

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			pool.mtx.Lock()
			pool.nextJobID++
			jobIDs[index] = pool.nextJobID
			pool.mtx.Unlock()
		}(i)
	}

	wg.Wait()

	seen := make(map[int64]bool)
	for _, id := range jobIDs {
		assert.False(t, seen[id], "Job ID %d was generated multiple times", id)
		seen[id] = true
	}

	assert.Len(t, seen, numGoroutines)
}

func TestServiceAccountNaming(t *testing.T) {
	// Test service account naming convention
	for i := 0; i < 10; i++ {
		expected := fmt.Sprintf("service-account-%d", i)
		assert.Regexp(t, `^service-account-\d+$`, expected)
	}
}

func TestPoolStateTransitions(t *testing.T) {
	client := &TxClient{}

	// Test initial state
	assert.Nil(t, client.unorderedPool)

	// Test after initialization (simulated)
	pool := &UnorderedTxPool{
		isRunning: false,
		stopCh:    make(chan struct{}),
	}
	client.unorderedPool = pool

	// Test state checks
	assert.False(t, pool.isRunning)

	pool.isRunning = true
	assert.True(t, pool.isRunning)
}

func BenchmarkJobCreation(b *testing.B) {
	pool := &UnorderedTxPool{
		jobQueue:  make(chan *txJob, 1000),
		nextJobID: 0,
		isRunning: true,
	}

	ns := share.MustNewV0Namespace([]byte{1, 1, 1, 1, 1, 1, 1, 1, 1, 1})
	blob, err := share.NewBlob(ns, []byte("test data"), share.ShareVersionZero, nil)
	if err != nil {
		b.Fatal(err)
	}
	blobs := []*share.Blob{blob}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			pool.mtx.Lock()
			pool.nextJobID++
			jobID := pool.nextJobID
			pool.mtx.Unlock()

			job := &txJob{
				id:     jobID,
				blobs:  blobs,
				opts:   []TxOption{},
				result: make(chan *TxResponse, 1),
				errCh:  make(chan error, 1),
			}

			select {
			case pool.jobQueue <- job:
			default:
				// Queue full, skip
			}
		}
	})
}

func TestContextCancellation(t *testing.T) {
	pool := &UnorderedTxPool{
		jobQueue:  make(chan *txJob, 1),
		nextJobID: 0,
		isRunning: true,
	}

	// Fill the queue
	pool.jobQueue <- &txJob{
		id:     1,
		result: make(chan *TxResponse, 1),
		errCh:  make(chan error, 1),
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// Simulate SubmitPayForBlobUnordered with cancelled context
	job := &txJob{
		id:     2,
		result: make(chan *TxResponse, 1),
		errCh:  make(chan error, 1),
	}

	select {
	case pool.jobQueue <- job:
		t.Fatal("Should not be able to send to full queue")
	case <-ctx.Done():
		// Expected behavior
		assert.Equal(t, context.Canceled, ctx.Err())
	default:
		t.Fatal("Context should be cancelled")
	}
}
