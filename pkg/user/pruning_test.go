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

	// Add 10 transactions to the tracker that are 10 minutes old
	var txsToBePruned int
	var txsNotReadyToBePruned int
	for i := 0; i < numTransactions; i++ {
		// 5 transactions will be pruned
		if i%2 == 0 {
			txClient.txTracker["tx"+fmt.Sprint(i)] = txInfo{
				signer:   "signer" + fmt.Sprint(i),
				sequence: uint64(i),
				timeStamp: time.Now().
					Add(-10 * time.Minute),
			}
			txsToBePruned++
		} else {
			txClient.txTracker["tx"+fmt.Sprint(i)] = txInfo{
				signer:   "signer" + fmt.Sprint(i),
				sequence: uint64(i),
				timeStamp: time.Now().
					Add(-5 * time.Minute),
			}
			txsNotReadyToBePruned++
		}
	}

	txTrackerBeforePruning := len(txClient.txTracker)

	// check that the tracker has 10 transactions
	require.Equal(t, 10, len(txClient.txTracker))
	txClient.pruneTxTracker()
	// check that the tracker prunes the transactions that are 10 minutes old
	// 5 transactions will be pruned
	require.Equal(t, txTrackerBeforePruning-txsToBePruned, txsToBePruned)
	// 5 transactions will not be pruned
	require.Equal(t, len(txClient.txTracker), txsNotReadyToBePruned)
}
