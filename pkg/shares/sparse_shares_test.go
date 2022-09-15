package shares

import (
	"fmt"
	"testing"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	coretypes "github.com/tendermint/tendermint/types"
)

func Test_parseSparseShares(t *testing.T) {
	// exactMsgShareSize is the length of message that will fit exactly into a single
	// share, accounting for namespace id and the length delimiter prepended to
	// each message
	const exactMsgShareSize = appconsts.SparseShareContentSize - 2

	type test struct {
		name     string
		msgSize  int
		msgCount int
	}

	// each test is ran twice, once using msgSize as an exact size, and again
	// using it as a cap for randomly sized leaves
	tests := []test{
		{"single small msg", 100, 1},
		{"many small msgs", 100, 10},
		{"single big msg", 1000, 1},
		{"many big msgs", 1000, 10},
		{"single exact size msg", exactMsgShareSize, 1},
		{"many exact size msgs", appconsts.SparseShareContentSize, 10},
	}

	for _, tc := range tests {
		tc := tc
		// run the tests with identically sized messages
		t.Run(fmt.Sprintf("%s identically sized ", tc.name), func(t *testing.T) {
			rawmsgs := make([]coretypes.Message, tc.msgCount)
			for i := 0; i < tc.msgCount; i++ {
				rawmsgs[i] = generateRandomMessage(tc.msgSize)
			}

			msgs := coretypes.Messages{MessagesList: rawmsgs}
			msgs.SortMessages()

			shares, _ := SplitMessages(nil, msgs.MessagesList)

			parsedMsgs, err := parseSparseShares(shares)
			if err != nil {
				t.Error(err)
			}

			// check that the namespaces and data are the same
			for i := 0; i < len(msgs.MessagesList); i++ {
				assert.Equal(t, msgs.MessagesList[i].NamespaceID, parsedMsgs[i].NamespaceID)
				assert.Equal(t, msgs.MessagesList[i].Data, parsedMsgs[i].Data)
			}
		})

		// run the same tests using randomly sized messages with caps of tc.msgSize
		t.Run(fmt.Sprintf("%s randomly sized", tc.name), func(t *testing.T) {
			msgs := generateRandomlySizedMessages(tc.msgCount, tc.msgSize)
			shares, _ := SplitMessages(nil, msgs.MessagesList)

			parsedMsgs, err := parseSparseShares(shares)
			if err != nil {
				t.Error(err)
			}

			// check that the namesapces and data are the same
			for i := 0; i < len(msgs.MessagesList); i++ {
				assert.Equal(t, msgs.MessagesList[i].NamespaceID, parsedMsgs[i].NamespaceID)
				assert.Equal(t, msgs.MessagesList[i].Data, parsedMsgs[i].Data)
			}
		})
	}
}

func TestParsePaddedMsg(t *testing.T) {
	msgWr := NewSparseShareSplitter()
	randomSmallMsg := generateRandomMessage(100)
	randomLargeMsg := generateRandomMessage(10000)
	msgs := coretypes.Messages{
		MessagesList: []coretypes.Message{
			randomSmallMsg,
			randomLargeMsg,
		},
	}
	msgs.SortMessages()
	msgWr.Write(msgs.MessagesList[0])
	msgWr.WriteNamespacedPaddedShares(4)
	msgWr.Write(msgs.MessagesList[1])
	msgWr.WriteNamespacedPaddedShares(10)
	pmsgs, err := parseSparseShares(msgWr.Export().RawShares())
	require.NoError(t, err)
	require.Equal(t, msgs.MessagesList, pmsgs)
}
