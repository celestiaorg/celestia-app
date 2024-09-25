package user

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPruningInTxTracker(t *testing.T) {
	txClient := &TxClient{
		txTracker: make(map[string]txInfo),
	}
	numTransactions := 10

	// Add 10 transactions to the tracker that are 10 and 5 minutes old
	var txsToBePruned int
	var txsNotReadyToBePruned int
	for i := 0; i < numTransactions; i++ {
		// 5 transactions will be pruned
		if i%2 == 0 {
			txClient.txTracker["tx"+fmt.Sprint(i)] = txInfo{
				signer:   "signer" + fmt.Sprint(i),
				sequence: uint64(i),
				timestamp: time.Now().
					Add(-10 * time.Minute),
			}
			txsToBePruned++
		} else {
			txClient.txTracker["tx"+fmt.Sprint(i)] = txInfo{
				signer:   "signer" + fmt.Sprint(i),
				sequence: uint64(i),
				timestamp: time.Now().
					Add(-5 * time.Minute),
			}
			txsNotReadyToBePruned++
		}
	}

	txTrackerBeforePruning := len(txClient.txTracker)

	// All transactions were indexed
	require.Equal(t, numTransactions, len(txClient.txTracker))
	txClient.pruneTxTracker()
	// Prunes the transactions that are 10 minutes old
	// 5 transactions will be pruned
	require.Equal(t, txsToBePruned, txTrackerBeforePruning-txsToBePruned)
	require.Equal(t, len(txClient.txTracker), txsNotReadyToBePruned)
}
