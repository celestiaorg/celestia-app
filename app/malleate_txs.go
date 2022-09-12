package app

import (
	"bytes"
	"errors"
	"sort"

	"github.com/celestiaorg/celestia-app/pkg/shares"
	"github.com/celestiaorg/celestia-app/x/payment/types"
	"github.com/cosmos/cosmos-sdk/client"
	core "github.com/tendermint/tendermint/proto/tendermint/types"
)

func malleateTxs(
	txConf client.TxConfig,
	squareSize uint64,
	txs parsedTxs,
	evd core.EvidenceList,
) ([][]byte, []*core.Message, error) {
	// trackedMessage keeps track of the pfd from which it was malleated from so
	// that we can wrap that pfd with appropriate share index
	type trackedMessage struct {
		message     *core.Message
		parsedIndex int
	}

	// malleate any malleable txs while also keeping track of the original order
	// and tagging the resulting messages with a reverse index.
	var err error
	var trackedMsgs []trackedMessage
	for i, pTx := range txs {
		if pTx.msg != nil {
			err = pTx.malleate(txConf, squareSize)
			if err != nil {
				txs.remove(i)
				continue
			}
			trackedMsgs = append(trackedMsgs, trackedMessage{message: pTx.message(), parsedIndex: i})
		}
	}

	// sort the messages so that we can create a data square whose messages are
	// ordered by namespace. This is a block validity rule, and will cause nmt
	// to panic if unsorted.
	sort.SliceStable(trackedMsgs, func(i, j int) bool {
		return bytes.Compare(trackedMsgs[i].message.NamespaceId, trackedMsgs[j].message.NamespaceId) < 0
	})

	// split the tracked messagse apart now that we know the order of the indexes
	msgs := make([]*core.Message, len(trackedMsgs))
	parsedTxReverseIndexes := make([]int, len(trackedMsgs))
	for i, tMsg := range trackedMsgs {
		msgs[i] = tMsg.message
		parsedTxReverseIndexes[i] = tMsg.parsedIndex
	}

	// the malleated transactions still need to be wrapped with the starting
	// share index of the message, which we still need to calculate. Here we
	// calculate the exact share counts used by the different types of block
	// data in order to get an accurate index.
	compactShareCount := calculateCompactShareCount(txs, evd, int(squareSize))
	msgShareCounts := shares.MessageShareCountsFromMessages(msgs)
	// calculate the indexes that will be used for each message
	_, indexes := shares.MsgSharesUsedNonInteractiveDefaults(compactShareCount, int(squareSize), msgShareCounts...)
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

	return processedTxs, msgs, err
}

func (p *parsedTx) malleate(txConf client.TxConfig, squareSize uint64) error {
	if p.msg == nil || p.tx == nil {
		return errors.New("can only malleate a tx with a MsgWirePayForData")
	}

	// parse wire message and create a single message
	_, unsignedPFD, sig, err := types.ProcessWirePayForData(p.msg, squareSize)
	if err != nil {
		return err
	}

	// create the signed PayForData using the fees, gas limit, and sequence from
	// the original transaction, along with the appropriate signature.
	signedTx, err := types.BuildPayForDataTxFromWireTx(p.tx, txConf.NewTxBuilder(), sig, unsignedPFD)
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
