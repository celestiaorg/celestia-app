package app

import (
	"github.com/celestiaorg/celestia-app/app/encoding"
	coretypes "github.com/tendermint/tendermint/types"

	"github.com/celestiaorg/celestia-app/testutil/blobfactory"
)

func generateMixedParsedTxs(normalTxCount, pfbCount, pfbSize int) []parsedTx {
	encCfg := encoding.MakeConfig(ModuleEncodingRegisters...)
	pfbTxs := blobfactory.RandBlobTxs(encCfg.TxConfig.TxEncoder(), pfbCount, pfbSize)
	normieTxs := blobfactory.GenerateManyRawSendTxs(encCfg.TxConfig, normalTxCount)
	txs := append(append(
		make([]coretypes.Tx, 0, len(pfbTxs)+len(normieTxs)),
		normieTxs...),
		pfbTxs...,
	)
	return parseTxs(encCfg.TxConfig, coretypes.Txs(txs).ToSliceOfBytes())
}

func generateParsedTxsWithNIDs(nids [][]byte, sizes []int) []parsedTx {
	encCfg := encoding.MakeConfig(ModuleEncodingRegisters...)
	txs := blobfactory.RandBlobTxsWithNamespaces(encCfg.TxConfig.TxEncoder(), nids, sizes)
	return parseTxs(encCfg.TxConfig, coretypes.Txs(txs).ToSliceOfBytes())
}
