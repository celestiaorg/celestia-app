package app

import (
	"github.com/celestiaorg/celestia-app/app/encoding"
	coretypes "github.com/tendermint/tendermint/types"

	"github.com/celestiaorg/celestia-app/testutil/blobfactory"
)

func generateParsedTxs(count, size int) []parsedTx {
	encCfg := encoding.MakeConfig(ModuleEncodingRegisters...)
	txs := blobfactory.RandBlobTxs(encCfg.TxConfig.TxEncoder(), count, size)
	return parseTxs(encCfg.TxConfig, coretypes.Txs(txs).ToSliceOfBytes())
}

func generateParsedTxsWithNIDs(nids [][]byte, sizes []int) []parsedTx {
	encCfg := encoding.MakeConfig(ModuleEncodingRegisters...)
	txs := blobfactory.RandBlobTxsWithNamespaces(encCfg.TxConfig.TxEncoder(), nids, sizes)
	return parseTxs(encCfg.TxConfig, coretypes.Txs(txs).ToSliceOfBytes())
}
