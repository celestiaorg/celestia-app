package app

import (
	"bytes"
	"crypto/sha256"
	"math/bits"
	"sort"

	"github.com/celestiaorg/celestia-app/x/payment/types"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/auth/signing"
	"github.com/tendermint/tendermint/pkg/consts"
	core "github.com/tendermint/tendermint/proto/tendermint/types"
	coretypes "github.com/tendermint/tendermint/types"
)

// SplitShares uses the provided block data to create a flattened data square.
// Any MsgWirePayForDatas are malleated, and their corresponding
// MsgPayForData and Message are written atomically. If there are
// transactions that will node fit in the given square size, then they are
// discarded. This is reflected in the returned block data. Note: pointers to
// block data are only used to avoid dereferening, not because we need the block
// data to be mutable.
func SplitShares(txConf client.TxConfig, squareSize uint64, data *core.Data) ([][]byte, *core.Data) {
	processedTxs := make([][]byte, 0)
	messages := core.Messages{}

	sqwr := newShareSplitter(txConf, squareSize, data)

	for _, rawTx := range data.Txs {
		// decode the Tx
		tx, err := txConf.TxDecoder()(rawTx)
		if err != nil {
			continue
		}

		authTx, ok := tx.(signing.Tx)
		if !ok {
			continue
		}

		// skip txs that don't contain messages
		if !hasWirePayForData(authTx) {
			success, err := sqwr.writeTx(rawTx)
			if err != nil {
				continue
			}
			if !success {
				// the square is full
				break
			}
			processedTxs = append(processedTxs, rawTx)
			continue
		}

		// only support malleated transactions that contain a single sdk.Msg
		if len(authTx.GetMsgs()) != 1 {
			continue
		}

		msg := authTx.GetMsgs()[0]
		wireMsg, ok := msg.(*types.MsgWirePayForData)
		if !ok {
			continue
		}

		// run basic validation on the transaction (which include the wireMsg
		// above)
		err = authTx.ValidateBasic()
		if err != nil {
			continue
		}

		// attempt to malleate and write the resulting tx + msg to the square
		parentHash := sha256.Sum256(rawTx)
		success, malTx, message, err := sqwr.writeMalleatedTx(parentHash[:], authTx, wireMsg)
		if err != nil {
			continue
		}
		if !success {
			// the square is full, but we will attempt to continue to fill the
			// block until there are no tx left or no room for txs. While there
			// was not room for this particular tx + msg, there might be room
			// for other txs or even other smaller messages
			continue
		}
		processedTxs = append(processedTxs, malTx)
		messages.MessagesList = append(messages.MessagesList, message)
	}

	sort.Slice(messages.MessagesList, func(i, j int) bool {
		return bytes.Compare(messages.MessagesList[i].NamespaceId, messages.MessagesList[j].NamespaceId) < 0
	})

	return sqwr.export(), &core.Data{
		Txs:      processedTxs,
		Messages: messages,
		Evidence: data.Evidence,
	}
}

// shareSplitter write a data square using provided block data. It also ensures
// that message and their corresponding txs get written to the square
// atomically.
type shareSplitter struct {
	txWriter  *coretypes.ContiguousShareWriter
	msgWriter *coretypes.MessageShareWriter

	// Since evidence will always be included in a block, we do not need to
	// generate these share lazily. Therefore instead of a ContiguousShareWriter
	// we use the normal eager mechanism
	evdShares [][]byte

	squareSize    uint64
	maxShareCount int
	txConf        client.TxConfig
}

func newShareSplitter(txConf client.TxConfig, squareSize uint64, data *core.Data) *shareSplitter {
	sqwr := shareSplitter{
		squareSize:    squareSize,
		maxShareCount: int(squareSize * squareSize),
		txConf:        txConf,
	}

	evdData := new(coretypes.EvidenceData)
	err := evdData.FromProto(&data.Evidence)
	if err != nil {
		panic(err)
	}
	sqwr.evdShares = evdData.SplitIntoShares().RawShares()

	sqwr.txWriter = coretypes.NewContiguousShareWriter(consts.TxNamespaceID)
	sqwr.msgWriter = coretypes.NewMessageShareWriter()

	return &sqwr
}

// writeTx marshals the tx and lazily writes it to the square. Returns true if
// the write was successful, false if there was not enough room in the square.
func (sqwr *shareSplitter) writeTx(tx []byte) (ok bool, err error) {
	delimTx, err := coretypes.Tx(tx).MarshalDelimited()
	if err != nil {
		return false, err
	}

	if !sqwr.hasRoomForTx(delimTx) {
		return false, nil
	}

	sqwr.txWriter.Write(delimTx)
	return true, nil
}

// writeMalleatedTx malleates a MsgWirePayForData into a MsgPayForData and
// its corresponding message provided that it has a MsgPayForData for the
// preselected square size. Returns true if the write was successful, false if
// there was not enough room in the square.
func (sqwr *shareSplitter) writeMalleatedTx(
	parentHash []byte,
	tx signing.Tx,
	wpfd *types.MsgWirePayForData,
) (ok bool, malleatedTx coretypes.Tx, msg *core.Message, err error) {
	// parse wire message and create a single message
	coreMsg, unsignedPFD, sig, err := types.ProcessWirePayForData(wpfd, sqwr.squareSize)
	if err != nil {
		return false, nil, nil, err
	}

	// create the signed PayForData using the fees, gas limit, and sequence from
	// the original transaction, along with the appropriate signature.
	signedTx, err := types.BuildPayForDataTxFromWireTx(tx, sqwr.txConf.NewTxBuilder(), sig, unsignedPFD)
	if err != nil {
		return false, nil, nil, err
	}

	rawProcessedTx, err := sqwr.txConf.TxEncoder()(signedTx)
	if err != nil {
		return false, nil, nil, err
	}

	wrappedTx, err := coretypes.WrapMalleatedTx(parentHash[:], rawProcessedTx)
	if err != nil {
		return false, nil, nil, err
	}

	// Check if we have room for both the tx and message. It is crucial that we
	// add both atomically, otherwise the block would be invalid.
	if !sqwr.hasRoomForBoth(wrappedTx, coreMsg.Data) {
		return false, nil, nil, nil
	}

	delimTx, err := wrappedTx.MarshalDelimited()
	if err != nil {
		return false, nil, nil, err
	}

	sqwr.txWriter.Write(delimTx)
	sqwr.msgWriter.Write(coretypes.Message{
		NamespaceID: coreMsg.NamespaceId,
		Data:        coreMsg.Data,
	})

	return true, wrappedTx, coreMsg, nil
}

func (sqwr *shareSplitter) hasRoomForBoth(tx, msg []byte) bool {
	currentShareCount, availableBytes := sqwr.shareCount()

	txBytesTaken := delimLen(uint64(len(tx))) + len(tx)

	maxTxSharesTaken := ((txBytesTaken - availableBytes) / consts.TxShareSize) + 1 // plus one becuase we have to add at least one share

	maxMsgSharesTaken := len(msg) / consts.MsgShareSize

	return currentShareCount+maxTxSharesTaken+maxMsgSharesTaken <= sqwr.maxShareCount
}

func (sqwr *shareSplitter) hasRoomForTx(tx []byte) bool {
	currentShareCount, availableBytes := sqwr.shareCount()

	bytesTaken := delimLen(uint64(len(tx))) + len(tx)
	if bytesTaken <= availableBytes {
		return true
	}

	maxSharesTaken := ((bytesTaken - availableBytes) / consts.TxShareSize) + 1 // plus one becuase we have to add at least one share

	return currentShareCount+maxSharesTaken <= sqwr.maxShareCount
}

func (sqwr *shareSplitter) shareCount() (count, availableTxBytes int) {
	txsShareCount, availableBytes := sqwr.txWriter.Count()
	return txsShareCount + len(sqwr.evdShares) + sqwr.msgWriter.Count(),
		availableBytes
}

func (sqwr *shareSplitter) export() [][]byte {
	count, availableBytes := sqwr.shareCount()
	// increment the count if there are any pending tx bytes
	if availableBytes < consts.TxShareSize {
		count++
	}
	shares := make([][]byte, sqwr.maxShareCount)

	txShares := sqwr.txWriter.Export().RawShares()
	txShareCount := len(txShares)
	copy(shares, txShares)

	evdShareCount := len(sqwr.evdShares)
	for i, evdShare := range sqwr.evdShares {
		shares[i+txShareCount] = evdShare
	}

	msgShares := sqwr.msgWriter.Export()
	msgShareCount := len(msgShares)
	for i, msgShare := range msgShares {
		shares[i+txShareCount+evdShareCount] = msgShare.Share
	}

	tailShares := coretypes.TailPaddingShares(sqwr.maxShareCount - count).RawShares()

	for i, tShare := range tailShares {
		d := i + txShareCount + evdShareCount + msgShareCount
		shares[d] = tShare
	}

	if len(shares[0]) == 0 {
		shares = coretypes.TailPaddingShares(consts.MinSharecount).RawShares()
	}

	return shares
}

func hasWirePayForData(tx sdk.Tx) bool {
	for _, msg := range tx.GetMsgs() {
		msgName := sdk.MsgTypeURL(msg)
		if msgName == types.URLMsgWirePayForData {
			return true
		}
	}
	return false
}

func delimLen(x uint64) int {
	return 8 - bits.LeadingZeros64(x)%8
}
