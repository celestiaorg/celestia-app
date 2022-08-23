package shares

import (
	"math/bits"

	"github.com/tendermint/tendermint/pkg/consts"
	core "github.com/tendermint/tendermint/proto/tendermint/types"
	coretypes "github.com/tendermint/tendermint/types"
)

// DelimLen calculates the length of the delimiter for a given message size
func DelimLen(x uint64) int {
	return 8 - bits.LeadingZeros64(x)%8
}

// MsgSharesUsed calculates the minimum number of shares a message will take up.
// It accounts for the necessary delimiter and potential padding.
func MsgSharesUsed(msgSize int) int {
	// add the delimiter to the message size
	msgSize = DelimLen(uint64(msgSize)) + msgSize
	shareCount := msgSize / consts.MsgShareSize
	// increment the share count if the message overflows the last counted share
	if msgSize%consts.MsgShareSize != 0 {
		shareCount++
	}
	return shareCount
}

func MessageShareCountsFromMessages(msgs []*core.Message) []int {
	e := make([]int, len(msgs))
	for i, msg := range msgs {
		e[i] = MsgSharesUsed(len(msg.Data))
	}
	return e
}

func isPowerOf2(v uint64) bool {
	return v&(v-1) == 0 && v != 0
}

func MessagesToProto(msgs []coretypes.Message) []*core.Message {
	protoMsgs := make([]*core.Message, len(msgs))
	for i, msg := range msgs {
		protoMsgs[i] = &core.Message{
			NamespaceId: msg.NamespaceID,
			Data:        msg.Data,
		}
	}
	return protoMsgs
}

func MessagesFromProto(msgs []*core.Message) []coretypes.Message {
	protoMsgs := make([]coretypes.Message, len(msgs))
	for i, msg := range msgs {
		protoMsgs[i] = coretypes.Message{
			NamespaceID: msg.NamespaceId,
			Data:        msg.Data,
		}
	}
	return protoMsgs
}

func TxsToBytes(txs coretypes.Txs) [][]byte {
	e := make([][]byte, len(txs))
	for i, tx := range txs {
		e[i] = []byte(tx)
	}
	return e
}

func TxsFromBytes(txs [][]byte) coretypes.Txs {
	e := make(coretypes.Txs, len(txs))
	for i, tx := range txs {
		e[i] = coretypes.Tx(tx)
	}
	return e
}
