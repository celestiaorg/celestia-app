package app

import (
	blobtypes "github.com/celestiaorg/celestia-app/x/blob/types"
	"github.com/cosmos/cosmos-sdk/client"
	tmbytes "github.com/tendermint/tendermint/libs/bytes"
	"github.com/tendermint/tendermint/libs/log"
	coretypes "github.com/tendermint/tendermint/types"
)

// parsedTx is an internal struct that keeps track of all transactions.
type parsedTx struct {
	// normalTx is the raw unmodified transaction only filled if the transaction does not have any blobs
	// attached
	normalTx []byte
	// blobTx is the processed blob transaction. this field is only filled if
	// the transaction has blobs attached
	blobTx     blobtypes.ProcessedBlobTx
	shareIndex uint32
}

// parseTxs decodes raw tendermint txs along with checking for and processing
// blob transactions.
func parseTxs(txcfg client.TxConfig, rawTxs [][]byte) []parsedTx {
	parsedTxs := make([]parsedTx, len(rawTxs))
	for i, rawTx := range rawTxs {
		bTx, isBlob := coretypes.UnmarshalBlobTx(rawTx)
		if !isBlob {
			parsedTxs[i] = parsedTx{normalTx: rawTx}
			continue
		}
		pBTx, err := blobtypes.ProcessBlobTx(txcfg, bTx)
		if err != nil {
			// this should never occur, as we should be performing this exact
			// same check during CheckTx
			continue
		}
		parsedTxs[i] = parsedTx{blobTx: pBTx}
	}
	return parsedTxs
}

// processTxs wraps the parsed transactions with the attached share index
func processTxs(logger log.Logger, txs []parsedTx) [][]byte {
	processedTxs := make([][]byte, 0)
	for _, pTx := range txs {
		if len(pTx.normalTx) != 0 {
			processedTxs = append(processedTxs, pTx.normalTx)
			continue
		}

		// if this is a blob transaction, then we need to encode and wrap the
		// underlying MsgPFB containing transaction
		wTx, err := coretypes.MarshalIndexWrapper(pTx.shareIndex, pTx.blobTx.Tx)
		if err != nil {
			// note: Its not safe to bubble this error up and stop the block
			// creation process.
			logger.Error(
				"failure to wrap an otherwise valid BlobTx when creating a block: %v",
				tmbytes.HexBytes(coretypes.Tx(pTx.blobTx.Tx).Hash()),
			)
			continue
		}

		processedTxs = append(processedTxs, wTx)
	}
	return processedTxs
}
