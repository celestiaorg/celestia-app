package app

import (
	"crypto/sha256"

	"github.com/celestiaorg/celestia-app/x/payment/types"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/auth/signing"
	"github.com/tendermint/tendermint/pkg/consts"
	core "github.com/tendermint/tendermint/proto/tendermint/types"
	coretypes "github.com/tendermint/tendermint/types"
)

// WriteSquare uses the provided block data to create a flattened data square.
// Any MsgWirePayForMessages are malleated, and their corresponding
// MsgPayForMessage and Message are written atomically. If there are
// transactions that will node fit in the given square size, then they are
// discarded. This is reflected in the returned block data. Note: pointers to
// block data are only used to avoid dereferening, not because we need the block
// data to be mutable.
func WriteSquare(txConf client.TxConfig, squareSize uint64, data *core.Data) ([][]byte, *core.Data, error) {
	var (
		processedTxs [][]byte
		messages     core.Messages
	)
	sqwr, err := newSquareWriter(txConf, squareSize, data)
	if err != nil {
		return nil, nil, err
	}
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

		// write the tx to the square if it normal
		if !hasWirePayForMessage(authTx) {
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
		wireMsg, ok := msg.(*types.MsgWirePayForMessage)
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
}

// squareWriter write a data square using provided block data. It also ensures
// that message and their corresponding txs get written to the square
// atomically.
type squareWriter struct {
	txWriter  *coretypes.ContiguousShareWriter
	msgWriter *coretypes.MessageShareWriter

	// we aren't doing anything with evidence, so we don't need to lazily
	// generate those shares lazily
	evdShares [][]byte

	squareSize    uint64
	maxShareCount int
	txConf        client.TxConfig
}

func newSquareWriter(txConf client.TxConfig, squareSize uint64, data *core.Data) (*squareWriter, error) {
	sqwr := squareWriter{
		squareSize:    squareSize,
		maxShareCount: int(squareSize * squareSize),
		txConf:        txConf,
	}

	evdData := new(coretypes.EvidenceData)
	err := evdData.FromProto(&data.Evidence)
	if err != nil {
		return nil, err
	}
	if evdData != nil {
		sqwr.evdShares = evdData.SplitIntoShares().RawShares()
	}

	sqwr.txWriter = coretypes.NewContiguousShareWriter(consts.TxNamespaceID)
	sqwr.msgWriter = coretypes.NewMessageShareWriter()

	return &sqwr, nil
}

// writeTx marshals the tx and lazily writes it to the square. Returns true if
// the write was successful, false if there was not enough room in the square.
func (sqwr *squareWriter) writeTx(tx []byte) (ok bool, err error) {
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

// writeMalleated malleates a MsgWirePayForMessage into a MsgPayForMessage and
// its corresponding message provided that it has a MsgPayForMessage for the
// preselected square size. Returns true if the write was successful, false if
// there was not enough room in the square.
func (sqwr *squareWriter) writeMalleatedTx(
	parentHash []byte,
	tx signing.Tx,
	wpfm *types.MsgWirePayForMessage,
) (ok bool, malleatedTx coretypes.Tx, msg *core.Message, err error) {
	// parse wire message and create a single message
	coreMsg, unsignedPFM, sig, err := types.ProcessWirePayForMessage(wpfm, sqwr.squareSize)
	if err != nil {
		return false, nil, nil, err
	}

	// create the signed PayForMessage using the fees, gas limit, and sequence from
	// the original transaction, along with the appropriate signature.
	signedTx, err := types.BuildPayForMessageTxFromWireTx(tx, sqwr.txConf.NewTxBuilder(), sig, unsignedPFM)
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

	// check if we have room for both the tx and message it is crucial that we
	// add both atomically, otherwise the block is invalid

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

	return
}

func (sqwr *squareWriter) hasRoomForBoth(tx, msg []byte) bool {
	maxTxSharesTaken := (len(tx) / consts.TxShareSize) + 1
	maxMsgSharesTaken := (len(msg) / consts.MsgShareSize) + 1
	if sqwr.shareCount()+maxTxSharesTaken+maxMsgSharesTaken > sqwr.maxShareCount {
		return false
	}
	return true
}

func (sqwr *squareWriter) hasRoomForTx(tx []byte) bool {
	maxSharesTaken := (len(tx) / consts.TxShareSize) + 1
	// check if we have room
	if sqwr.shareCount()+maxSharesTaken > sqwr.maxShareCount {
		// we don't (yet) have a way of knowing how many bytes are left in the
		// pending share, so instead we assume we don't have room. Not maximally
		// efficient as it is still possible that the tx could fit
		return false
	}
	return true
}

func (sqwr *squareWriter) shareCount() int {
	return sqwr.txWriter.Count() + len(sqwr.evdShares) + sqwr.msgWriter.Count()
}

func (sqwr *squareWriter) export() [][]byte {
	shares := make([][]byte, sqwr.shareCount())

	txShares := sqwr.txWriter.Export().RawShares()
	txShareCount := len(txShares)
	for i, txShare := range txShares {
		shares[i] = txShare
	}

	evdShareCount := len(sqwr.evdShares)
	for i, evdShare := range sqwr.evdShares {
		shares[i+txShareCount] = evdShare
	}

	for i, msgShare := range sqwr.msgWriter.Export().RawShares() {
		shares[i+txShareCount+evdShareCount] = msgShare
	}

	return shares
}

func hasWirePayForMessage(tx sdk.Tx) bool {
	for _, msg := range tx.GetMsgs() {
		msgName := sdk.MsgTypeURL(msg)
		if msgName == types.URLMsgWirePayforMessage {
			return true
		}
	}
	return false
}
