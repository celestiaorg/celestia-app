package testfactory

import (
	crand "crypto/rand"

	"github.com/cometbft/cometbft/types"
)

func GenerateRandomTxs(count, size int) types.Txs {
	txs := make(types.Txs, count)
	for i := range count {
		tx := make([]byte, size)
		_, err := crand.Read(tx)
		if err != nil {
			panic(err)
		}
		txs[i] = tx
	}
	return txs
}
