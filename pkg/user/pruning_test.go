package user

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPruningInTxTracker(t *testing.T) {
	txClient := &TxClient{
		TxTracker: NewTxTracker(),
	}
	numTransactions := 10

	// Add 10 transactions to the tracker that are 10 and 5 minutes old
	var txsToBePruned int
	var txsNotReadyToBePruned int
	for i := range numTransactions {
		// 5 transactions will be pruned
		if i%2 == 0 {
			txClient.TxTracker.trackTransaction("signer"+fmt.Sprint(i), uint64(i), "tx"+fmt.Sprint(i), []byte(fmt.Sprintf("tx%d", i)))
			txsToBePruned++
		} else {
			txClient.TxTracker.trackTransaction("signer"+fmt.Sprint(i), uint64(i), "tx"+fmt.Sprint(i), []byte(fmt.Sprintf("tx%d", i)))
			txsNotReadyToBePruned++
		}
	}

	txTrackerBeforePruning := len(txClient.TxTracker.TxQueue)

	// All transactions were indexed
	require.Equal(t, numTransactions, len(txClient.TxTracker.TxQueue))
	txClient.TxTracker.pruneTxTracker()
	// Prunes the transactions that are 10 minutes old
	// 5 transactions will be pruned
	require.Equal(t, txsNotReadyToBePruned, txTrackerBeforePruning-txsToBePruned)
	require.Equal(t, len(txClient.TxTracker.TxQueue), txsNotReadyToBePruned)
}
