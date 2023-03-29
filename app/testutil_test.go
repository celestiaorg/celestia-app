package app

import (
	"testing"

	"github.com/celestiaorg/celestia-app/app/encoding"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	coretypes "github.com/tendermint/tendermint/types"

	appns "github.com/celestiaorg/celestia-app/pkg/namespace"
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

// generateBlobTxsWithNamespaces will generate len(namespaces) BlobTxs with
// len(blobSizes[i]) number of blobs per BlobTx. Note: not suitable for using in
// prepare or process proposal, as the signatures will be invalid since this
// does not query for relevant account numbers or sequences.
func generateBlobTxsWithNamespaces(t *testing.T, namespaces []appns.Namespace, blobSizes [][]int) []tmproto.BlobTx {
	encCfg := encoding.MakeConfig(ModuleEncodingRegisters...)
	const acc = "signer"
	kr := testfactory.GenerateKeyring(acc)
	txs := blobfactory.ManyMultiBlobTx(
		t,
		encCfg.TxConfig.TxEncoder(),
		kr,
		"chainid",
		blobfactory.Repeat(acc, len(blobSizes)),
		blobfactory.Repeat(blobfactory.AccountInfo{}, len(blobSizes)),
		blobfactory.NestedBlobs(t, namespaces, blobSizes),
	)
	_, blobTxs := separateTxs(encCfg.TxConfig, txs)
	return blobTxs
}

func generateNormalTxs(count int) [][]byte {
	encCfg := encoding.MakeConfig(ModuleEncodingRegisters...)
	normieTxs := blobfactory.GenerateManyRawSendTxs(encCfg.TxConfig, count)
	return coretypes.Txs(normieTxs).ToSliceOfBytes()
}
