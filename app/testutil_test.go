package app

import (
	"testing"

	"github.com/celestiaorg/celestia-app/app/encoding"
	"github.com/celestiaorg/celestia-app/pkg/transaction"
	coretypes "github.com/tendermint/tendermint/types"

	"github.com/celestiaorg/celestia-app/testutil/blobfactory"
	"github.com/celestiaorg/celestia-app/testutil/testfactory"
)

func generateMixedParsedTxs(normalTxCount, pfbCount, pfbSize int) []transaction.ParsedTx {
	encCfg := encoding.MakeConfig(ModuleEncodingRegisters...)
	pfbTxs := blobfactory.RandBlobTxs(encCfg.TxConfig.TxEncoder(), pfbCount, pfbSize)
	normieTxs := blobfactory.GenerateManyRawSendTxs(encCfg.TxConfig, normalTxCount)
	txs := append(append(
		make([]coretypes.Tx, 0, len(pfbTxs)+len(normieTxs)),
		normieTxs...),
		pfbTxs...,
	)
	return transaction.ParseTxs(encCfg.TxConfig, coretypes.Txs(txs).ToSliceOfBytes())
}

// generateParsedTxsWithNIDs will generate len(nids) parsed BlobTxs with
// len(blobSizes[i]) number of blobs per BlobTx.
func generateParsedTxsWithNIDs(t *testing.T, nids [][]byte, blobSizes [][]int) []transaction.ParsedTx {
	encCfg := encoding.MakeConfig(ModuleEncodingRegisters...)
	const acc = "signer"
	kr := testfactory.GenerateKeyring(acc)
	txs := blobfactory.ManyMultiBlobTx(
		t,
		encCfg.TxConfig.TxEncoder(),
		kr,
		"chainid",
		blobfactory.Repeat(acc, len(blobSizes)),
		blobfactory.NestedBlobs(t, nids, blobSizes),
	)
	return transaction.ParseTxs(encCfg.TxConfig, txs)
}

func generateNormalParsedTxs(count int) []transaction.ParsedTx {
	encCfg := encoding.MakeConfig(ModuleEncodingRegisters...)
	normieTxs := blobfactory.GenerateManyRawSendTxs(encCfg.TxConfig, count)
	return transaction.ParseTxs(encCfg.TxConfig, coretypes.Txs(normieTxs).ToSliceOfBytes())
}
