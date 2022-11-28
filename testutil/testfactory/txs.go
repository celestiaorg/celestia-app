package testfactory

import (
	mrand "math/rand"

	"github.com/tendermint/tendermint/types"
)

func GenerateRandomlySizedTransactions(count, maxSize int) types.Txs {
	txs := make(types.Txs, count)
	for i := 0; i < count; i++ {
		size := mrand.Intn(maxSize)
		if size == 0 {
			size = 1
		}
		txs[i] = GenerateRandomTransactions(1, size)[0]
	}
	return txs
}

func GenerateRandomTransactions(count, size int) types.Txs {
	txs := make(types.Txs, count)
	for i := 0; i < count; i++ {
		tx := make([]byte, size)
		_, err := mrand.Read(tx)
		if err != nil {
			panic(err)
		}
		txs[i] = tx
	}
	return txs
}
