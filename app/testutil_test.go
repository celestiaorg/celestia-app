package app

import (
	"testing"

	"github.com/celestiaorg/celestia-app/app/encoding"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	coretypes "github.com/tendermint/tendermint/types"

	"github.com/celestiaorg/celestia-app/testutil/blobfactory"
	"github.com/celestiaorg/celestia-app/testutil/testfactory"
)

func generateMixedTxs(normalTxCount, pfbCount, pfbSize int) ([][]byte, []tmproto.BlobTx) {
	encCfg := encoding.MakeConfig(ModuleEncodingRegisters...)
	pfbTxs := blobfactory.RandBlobTxs(encCfg.TxConfig.TxEncoder(), pfbCount, pfbSize)
	normieTxs := blobfactory.GenerateManyRawSendTxs(encCfg.TxConfig, normalTxCount)
	txs := append(append(
		make([]coretypes.Tx, 0, len(pfbTxs)+len(normieTxs)),
		normieTxs...),
		pfbTxs...,
	)
	return separateTxs(encCfg.TxConfig, coretypes.Txs(txs).ToSliceOfBytes())
}

// generateBlobTxsWithNIDs will generate len(nids) BlobTxs with
// len(blobSizes[i]) number of blobs per BlobTx.
func generateBlobTxsWithNIDs(t *testing.T, nids [][]byte, blobSizes [][]int) []tmproto.BlobTx {
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
	_, blobTxs := separateTxs(encCfg.TxConfig, txs)
	return blobTxs
}

func generateNormalTxs(count int) [][]byte {
	encCfg := encoding.MakeConfig(ModuleEncodingRegisters...)
	normieTxs := blobfactory.GenerateManyRawSendTxs(encCfg.TxConfig, count)
	return coretypes.Txs(normieTxs).ToSliceOfBytes()
}
