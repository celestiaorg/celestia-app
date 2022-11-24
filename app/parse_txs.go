package app

import (
	blobtypes "github.com/celestiaorg/celestia-app/x/blob/types"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/tendermint/tendermint/libs/log"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	coretypes "github.com/tendermint/tendermint/types"
)

// parsedTx is an internal struct that keeps track of potentially valid txs and
// their wire messages if they have any.
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
		bTx, isBlob := coretypes.UnwrapBlobTx(rawTx)
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

func processTxs(logger log.Logger, txcfg client.TxConfig, txs []parsedTx) ([][]byte, []tmproto.Blob) {
	processedTxs := make([][]byte, 0)
	blobs := make([]tmproto.Blob, 0)
	for _, pTx := range txs {
		if pTx.normalTx != nil {
			processedTxs = append(processedTxs, pTx.normalTx)
			continue
		}

		// if this is a blob transaction, then we need to encode and wrap the
		// underlying MsgPFB containing transaction
		wTx, err := coretypes.WrapMalleatedTx(pTx.shareIndex, pTx.blobTx.Tx)
		if err != nil {
			// note: Its not safe to bubble this error up and stop the block
			// creation process.
			logger.Error("failure to wrap an otherwise valid BlobTx when creating a block")
			continue
		}

		processedTxs = append(processedTxs, wTx)
		// todo: add support for more than the first blob note: that its safe to
		// assume that there is at least one blob, as this is checked during
		// checkTx
		blobs = append(blobs, pTx.blobTx.Blobs[0])

	}
	return processedTxs, blobs
}
