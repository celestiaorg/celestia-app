package transaction

import (
	blobtypes "github.com/celestiaorg/celestia-app/x/blob/types"
	"github.com/cosmos/cosmos-sdk/client"
	tmbytes "github.com/tendermint/tendermint/libs/bytes"
	"github.com/tendermint/tendermint/libs/log"
	core "github.com/tendermint/tendermint/proto/tendermint/types"
	coretypes "github.com/tendermint/tendermint/types"
)

// ParsedTx is an internal struct that keeps track of all transactions.
type ParsedTx struct {
	// normalTx is the raw unmodified transaction only filled if the transaction does not have any blobs
	// attached
	NormalTx []byte
	// blobTx is the processed blob transaction. this field is only filled if
	// the transaction has blobs attached
	BlobTx       core.BlobTx
	ShareIndexes []uint32
}

func (p ParsedTx) IsNormalTx() bool {
	return len(p.NormalTx) != 0
}

func (p ParsedTx) IsBlobTx() bool {
	return !p.IsNormalTx()
}

// ParseTxs decodes raw tendermint txs along with checking for and processing
// blob transactions.
func ParseTxs(txcfg client.TxConfig, rawTxs [][]byte) []ParsedTx {
	parsedTxs := make([]ParsedTx, len(rawTxs))
	for i, rawTx := range rawTxs {
		bTx, isBlob := coretypes.UnmarshalBlobTx(rawTx)
		if !isBlob {
			parsedTxs[i] = ParsedTx{NormalTx: rawTx}
			continue
		}
		err := blobtypes.ValidateBlobTx(txcfg, bTx)
		if err != nil {
			// this should never occur, as we should be performing this exact
			// same check during CheckTx
			continue
		}
		parsedTxs[i] = ParsedTx{BlobTx: bTx}
	}
	return parsedTxs
}

// ProcessTxs wraps the parsed transactions with the attached share index
func ProcessTxs(logger log.Logger, txs []ParsedTx) [][]byte {
	processedTxs := make([][]byte, 0)
	for _, pTx := range txs {
		if len(pTx.NormalTx) != 0 {
			processedTxs = append(processedTxs, pTx.NormalTx)
			continue
		}

		// if this is a blob transaction, then we need to encode and wrap the
		// underlying MsgPFB containing transaction
		wTx, err := coretypes.MarshalIndexWrapper(pTx.BlobTx.Tx, pTx.ShareIndexes...)
		if err != nil {
			// note: Its not safe to bubble this error up and stop the block
			// creation process.
			logger.Error(
				"failure to wrap an otherwise valid BlobTx when creating a block: %v",
				tmbytes.HexBytes(coretypes.Tx(pTx.BlobTx.Tx).Hash()),
			)
			continue
		}

		processedTxs = append(processedTxs, wTx)
	}
	return processedTxs
}
