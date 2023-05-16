package testfactory

import (
	crand "crypto/rand"
	"math/rand"

	"github.com/tendermint/tendermint/types"
)

func GenerateRandomlySizedTxs(count, maxSize int) types.Txs {
	txs := make(types.Txs, count)
	for i := 0; i < count; i++ {
		size := rand.Intn(maxSize)
		if size == 0 {
			size = 1
		}
		txs[i] = GenerateRandomTxs(1, size)[0]
	}
	return txs
}

func GenerateRandomTxs(count, size int) types.Txs {
	txs := make(types.Txs, count)
	for i := 0; i < count; i++ {
		tx := make([]byte, size)
		_, err := crand.Read(tx)
		if err != nil {
			panic(err)
		}
		txs[i] = tx
	}
	return txs
}
