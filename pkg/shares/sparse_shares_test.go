package shares

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/nmt/namespace"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	coretypes "github.com/tendermint/tendermint/types"
)

func Test_parseSparseShares(t *testing.T) {
	// exactMsgShareSize is the length of message that will fit exactly into a
	// single share, accounting for namespace id and the length delimiter
	// prepended to each message. Note that the length delimiter can be 1 to 10
	// bytes (varint) but this test assumes it is 2 bytes.
	const exactMsgShareSize = appconsts.SparseShareContentSize - 2

	type test struct {
		name     string
		msgSize  int
		msgCount int
	}

	// each test is ran twice, once using msgSize as an exact size, and again
	// using it as a cap for randomly sized leaves
	tests := []test{
		{"single small msg", appconsts.SparseShareContentSize / 2, 1},
		{"many small msgs", appconsts.SparseShareContentSize / 2, 10},
		{"single big msg", appconsts.SparseShareContentSize * 4, 1},
		{"many big msgs", appconsts.SparseShareContentSize * 4, 10},
		{"single exact size msg", exactMsgShareSize, 1},
		{"many exact size msgs", appconsts.SparseShareContentSize, 10},
	}

	for _, tc := range tests {
		tc := tc
		// run the tests with identically sized messagses
		t.Run(fmt.Sprintf("%s identically sized ", tc.name), func(t *testing.T) {
			rawmsgs := make([]coretypes.Message, tc.msgCount)
			for i := 0; i < tc.msgCount; i++ {
				rawmsgs[i] = generateRandomMessage(tc.msgSize)
			}

			msgs := coretypes.Messages{MessagesList: rawmsgs}
			msgs.SortMessages()

			shares, _ := SplitMessages(0, nil, msgs.MessagesList, false)
			rawShares := ToBytes(shares)

			parsedMsgs, err := parseSparseShares(rawShares, appconsts.SupportedShareVersions)
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
			shares, _ := SplitMessages(0, nil, msgs.MessagesList, false)
			rawShares := make([][]byte, len(shares))
			for i, share := range shares {
				rawShares[i] = []byte(share)
			}

			parsedMsgs, err := parseSparseShares(rawShares, appconsts.SupportedShareVersions)
			if err != nil {
				t.Error(err)
			}

			// check that the namespaces and data are the same
			for i := 0; i < len(msgs.MessagesList); i++ {
				assert.Equal(t, msgs.MessagesList[i].NamespaceID, parsedMsgs[i].NamespaceID)
				assert.Equal(t, msgs.MessagesList[i].Data, parsedMsgs[i].Data)
			}
		})
	}
}

func Test_parseSparseSharesErrors(t *testing.T) {
	type testCase struct {
		name      string
		rawShares [][]byte
	}

	unsupportedShareVersion := 5
	infoByte, _ := NewInfoByte(uint8(unsupportedShareVersion), true)

	rawShare := []byte{}
	rawShare = append(rawShare, namespace.ID{1, 1, 1, 1, 1, 1, 1, 1}...)
	rawShare = append(rawShare, byte(infoByte))
	rawShare = append(rawShare, bytes.Repeat([]byte{0}, appconsts.ShareSize-len(rawShare))...)

	tests := []testCase{
		{
			"share with unsupported share version",
			[][]byte{rawShare},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(*testing.T) {
			_, err := parseSparseShares(tt.rawShares, appconsts.SupportedShareVersions)
			assert.Error(t, err)
		})
	}
}

func TestParsePaddedMsg(t *testing.T) {
	msgWr := NewSparseShareSplitter()
	randomSmallMsg := generateRandomMessage(appconsts.SparseShareContentSize / 2)
	randomLargeMsg := generateRandomMessage(appconsts.SparseShareContentSize * 4)
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
	shares := msgWr.Export()
	rawShares := ToBytes(shares)
	pmsgs, err := parseSparseShares(rawShares, appconsts.SupportedShareVersions)
	require.NoError(t, err)
	require.Equal(t, msgs.MessagesList, pmsgs)
}

func TestMsgShareContainsInfoByte(t *testing.T) {
	sss := NewSparseShareSplitter()
	smallMsg := generateRandomMessage(appconsts.SparseShareContentSize / 2)
	sss.Write(smallMsg)

	shares := sss.Export()

	got := shares[0][appconsts.NamespaceSize : appconsts.NamespaceSize+appconsts.ShareInfoBytes][0]

	isSequenceStart := true
	want, err := NewInfoByte(appconsts.ShareVersion, isSequenceStart)

	require.NoError(t, err)
	assert.Equal(t, byte(want), got)
}

func TestContiguousMsgShareContainsInfoByte(t *testing.T) {
	sss := NewSparseShareSplitter()
	longMsg := generateRandomMessage(appconsts.SparseShareContentSize * 4)
	sss.Write(longMsg)

	shares := sss.Export()

	// we expect longMsg to occupy more than one share
	assert.Condition(t, func() bool { return len(shares) > 1 })
	got := shares[1][appconsts.NamespaceSize : appconsts.NamespaceSize+appconsts.ShareInfoBytes][0]

	isSequenceStart := false
	want, err := NewInfoByte(appconsts.ShareVersion, isSequenceStart)

	require.NoError(t, err)
	assert.Equal(t, byte(want), got)
}
