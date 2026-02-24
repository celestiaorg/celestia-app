package benchmarks_test

import (
	"runtime"
	"sync"
	"testing"

	"github.com/celestiaorg/celestia-app/v7/pkg/user"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"github.com/stretchr/testify/require"
)

// txGeneratorFunc is a callback that creates a single signed transaction.
// The signer is unique to the calling goroutine. index is the global
// transaction index (0-based).
type txGeneratorFunc func(signer *user.Signer, account string, index int) ([]byte, error)

// generateSignedTxsInParallel creates count signed transactions by splitting
// the work across runtime.NumCPU() goroutines. Each goroutine gets its own
// user.Signer initialised to the correct starting sequence so that the
// resulting slice is identical (modulo non-deterministic fields like random
// addresses) to the sequential version.
//
// The caller provides a txGeneratorFunc that is responsible for building and
// signing one transaction via the per-worker Signer.
func generateSignedTxsInParallel(
	b *testing.B,
	kr keyring.Keyring,
	encCfg client.TxConfig,
	chainID string,
	accountName string,
	accountNumber uint64,
	startSequence uint64,
	count int,
	genFunc txGeneratorFunc,
) [][]byte {
	b.Helper()

	numWorkers := runtime.NumCPU()
	if numWorkers > count {
		numWorkers = count
	}

	rawTxs := make([][]byte, count)
	errs := make([]error, numWorkers)

	chunkSize := count / numWorkers
	remainder := count % numWorkers

	var wg sync.WaitGroup
	wg.Add(numWorkers)

	workerStart := 0
	for w := 0; w < numWorkers; w++ {
		// Distribute remainder across the first workers.
		workerCount := chunkSize
		if w < remainder {
			workerCount++
		}
		workerEnd := workerStart + workerCount
		workerSeq := startSequence + uint64(workerStart)
		workerIdx := w
		wStart := workerStart

		go func() {
			defer wg.Done()

			signer, err := user.NewSigner(
				kr, encCfg, chainID,
				user.NewAccount(accountName, accountNumber, workerSeq),
			)
			if err != nil {
				errs[workerIdx] = err
				return
			}

			for i := wStart; i < workerEnd; i++ {
				tx, err := genFunc(signer, accountName, i)
				if err != nil {
					errs[workerIdx] = err
					return
				}
				rawTxs[i] = tx
				if err := signer.IncrementSequence(accountName); err != nil {
					errs[workerIdx] = err
					return
				}
			}
		}()

		workerStart = workerEnd
	}

	wg.Wait()

	for _, err := range errs {
		require.NoError(b, err)
	}

	return rawTxs
}
