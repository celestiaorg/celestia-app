package app

import (
	"bytes"
	"errors"
	"sort"

	"github.com/celestiaorg/celestia-app/pkg/shares"
	"github.com/celestiaorg/celestia-app/x/blob/types"
	"github.com/cosmos/cosmos-sdk/client"
	core "github.com/tendermint/tendermint/proto/tendermint/types"
)

func malleateTxs(
	txConf client.TxConfig,
	squareSize uint64,
	txs parsedTxs,
) ([][]byte, []core.Blob, error) {
	// trackedBlob keeps track of the pfb from which it was malleated from so
	// that we can wrap that pfb with appropriate share index
	type trackedBlob struct {
		blob        *core.Blob
		parsedIndex int
	}

	// malleate any malleable txs while also keeping track of the original order
	// and tagging the resulting trackedBlob with a reverse index.
	var err error
	var trackedBlobs []trackedBlob
	for i, pTx := range txs {
		if pTx.msg != nil {
			err = pTx.malleate(txConf)
			if err != nil {
				txs.remove(i)
				continue
			}
			trackedBlobs = append(trackedBlobs, trackedBlob{blob: pTx.blob(), parsedIndex: i})
		}
	}

	// sort the trackedBlobs so that we can create a data square whose blobs are
	// ordered by namespace. This is a block validity rule, and will cause nmt
	// to panic if unsorted.
	sort.SliceStable(trackedBlobs, func(i, j int) bool {
		return bytes.Compare(trackedBlobs[i].blob.NamespaceId, trackedBlobs[j].blob.NamespaceId) < 0
	})

	// split the tracked messagse apart now that we know the order of the indexes
	blobs := make([]core.Blob, len(trackedBlobs))
	parsedTxReverseIndexes := make([]int, len(trackedBlobs))
	for i, trackedBlob := range trackedBlobs {
		blobs[i] = *trackedBlob.blob
		parsedTxReverseIndexes[i] = trackedBlob.parsedIndex
	}

	// the malleated transactions still need to be wrapped with the starting
	// share index of the blob, which we still need to calculate. Here we
	// calculate the exact share counts used by the different types of block
	// data in order to get an accurate index.
	compactShareCount := calculateCompactShareCount(txs, int(squareSize))
	blobShareCounts := shares.BlobShareCountsFromBlobs(blobs)
	// calculate the indexes that will be used for each blob
	_, indexes := shares.BlobSharesUsedNonInteractiveDefaults(compactShareCount, int(squareSize), blobShareCounts...)
	for i, reverseIndex := range parsedTxReverseIndexes {
		wrappedMalleatedTx, err := txs[reverseIndex].wrap(indexes[i])
		if err != nil {
			return nil, nil, err
		}
		txs[reverseIndex].malleatedTx = wrappedMalleatedTx
	}

	// bring together the malleated and non malleated txs
	processedTxs := make([][]byte, len(txs))
	for i, t := range txs {
		if t.malleatedTx != nil {
			processedTxs[i] = t.malleatedTx
		} else {
			processedTxs[i] = t.rawTx
		}
	}

	return processedTxs, blobs, err
}

func (p *parsedTx) malleate(txConf client.TxConfig) error {
	if p.msg == nil || p.tx == nil {
		return errors.New("can only malleate a tx with a MsgWirePayForBlob")
	}

	_, unsignedMPFB, sig, err := types.ProcessWireMsgPayForBlob(p.msg)
	if err != nil {
		return err
	}

	// create the signed PayForBlob using the fees, gas limit, and sequence from
	// the original transaction, along with the appropriate signature.
	signedTx, err := types.BuildPayForBlobTxFromWireTx(p.tx, txConf.NewTxBuilder(), sig, unsignedMPFB)
	if err != nil {
		return err
	}

	rawProcessedTx, err := txConf.TxEncoder()(signedTx)
	if err != nil {
		return err
	}

	p.malleatedTx = rawProcessedTx
	return nil
}
