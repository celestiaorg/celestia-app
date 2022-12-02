package app

import (
	"github.com/celestiaorg/celestia-app/app/encoding"
	"github.com/celestiaorg/celestia-app/pkg/shares"
	"github.com/tendermint/tendermint/libs/log"
	core "github.com/tendermint/tendermint/proto/tendermint/types"
	coretypes "github.com/tendermint/tendermint/types"

	"github.com/celestiaorg/celestia-app/testutil/blobfactory"
)

func splitParsedTxs(ptxs []parsedTx) ([]shares.Share, error) {
	// estimate the square size. This estimation errors on the side of larger
	// squares but can only return values within the min and max square size.
	squareSize, nonreservedStart := estimateSquareSize(ptxs)

	addShareIndexes(squareSize, nonreservedStart, ptxs)

	processedTxs, blobs := processTxs(log.NewNopLogger(), ptxs)

	blockData := core.Data{
		Txs:        processedTxs,
		Blobs:      blobs,
		SquareSize: squareSize,
	}

	coreData, err := coretypes.DataFromProto(&blockData)
	if err != nil {
		panic(err)
	}

	return shares.Split(coreData, true)
}

func generateParsedTxs(count, size int) []parsedTx {
	encCfg := encoding.MakeConfig(ModuleEncodingRegisters...)
	txs := blobfactory.RandBlobTxs(encCfg.TxConfig.TxEncoder(), count, size)
	return parseTxs(encCfg.TxConfig, coretypes.Txs(txs).ToSliceOfBytes())
}

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
