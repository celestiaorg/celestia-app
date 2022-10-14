package shares

import (
	"math/rand"
	"reflect"
	"testing"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/nmt/namespace"
	tmrand "github.com/tendermint/tendermint/libs/rand"
	"github.com/tendermint/tendermint/types"
)

func TestParseShares(t *testing.T) {
	type testCase struct {
		name      string
		shares    [][]byte
		want      []ShareSequence
		expectErr bool
	}

	start := true
	messageOneNamespace := namespace.ID{1, 1, 1, 1, 1, 1, 1, 1}
	messageTwoNamespace := namespace.ID{2, 2, 2, 2, 2, 2, 2, 2}

	transactionShares := SplitTxs(generateRandomTxs(2, 1000))
	transactionShareStart := transactionShares[0]
	transactionShareContinuation := transactionShares[1]

	messageOneShares, err := SplitMessages(0, []uint32{}, []types.Message{generateRandomMessageWithNamespace(messageOneNamespace, 1000)}, false)
	if err != nil {
		t.Fatal(err)
	}
	messageOneStart := messageOneShares[0]
	messageOneContinuation := messageOneShares[1]

	messageTwoShares, err := SplitMessages(0, []uint32{}, []types.Message{generateRandomMessageWithNamespace(messageTwoNamespace, 1000)}, false)
	if err != nil {
		t.Fatal(err)
	}
	messageTwoStart := messageTwoShares[0]
	messageTwoContinuation := messageTwoShares[1]

	invalidShare := generateRawShare(messageOneNamespace, start)
	invalidShare = append(invalidShare, []byte{0}...)

	tests := []testCase{
		{
			"empty",
			[][]byte{},
			[]ShareSequence{},
			false,
		},
		{
			"one transaction share",
			[][]byte{transactionShareStart},
			[]ShareSequence{{NamespaceID: appconsts.TxNamespaceID, Shares: []Share{transactionShareStart}}},
			false,
		},
		{
			"two transaction shares",
			[][]byte{transactionShareStart, transactionShareContinuation},
			[]ShareSequence{{NamespaceID: appconsts.TxNamespaceID, Shares: []Share{transactionShareStart, transactionShareContinuation}}},
			false,
		},
		{
			"one message share",
			[][]byte{messageOneStart},
			[]ShareSequence{{NamespaceID: messageOneNamespace, Shares: []Share{messageOneStart}}},
			false,
		},
		{
			"two message shares",
			[][]byte{messageOneStart, messageOneContinuation},
			[]ShareSequence{{NamespaceID: messageOneNamespace, Shares: []Share{messageOneStart, messageOneContinuation}}},
			false,
		},
		{
			"two messages with two shares each",
			[][]byte{messageOneStart, messageOneContinuation, messageTwoStart, messageTwoContinuation},
			[]ShareSequence{
				{NamespaceID: messageOneNamespace, Shares: []Share{messageOneStart, messageOneContinuation}},
				{NamespaceID: messageTwoNamespace, Shares: []Share{messageTwoStart, messageTwoContinuation}},
			},
			false,
		},
		{
			"one transaction, one message",
			[][]byte{transactionShareStart, messageOneStart},
			[]ShareSequence{
				{NamespaceID: appconsts.TxNamespaceID, Shares: []Share{transactionShareStart}},
				{NamespaceID: messageOneNamespace, Shares: []Share{messageOneStart}},
			},
			false,
		},
		{
			"one transaction, two messages",
			[][]byte{transactionShareStart, messageOneStart, messageTwoStart},
			[]ShareSequence{
				{NamespaceID: appconsts.TxNamespaceID, Shares: []Share{transactionShareStart}},
				{NamespaceID: messageOneNamespace, Shares: []Share{messageOneStart}},
				{NamespaceID: messageTwoNamespace, Shares: []Share{messageTwoStart}},
			},
			false,
		},
		{
			"one share with invalid size",
			[][]byte{invalidShare},
			[]ShareSequence{},
			true,
		},
		{
			"message one start followed by message two continuation",
			[][]byte{messageOneStart, messageTwoContinuation},
			[]ShareSequence{},
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseShares(tt.shares)
			if tt.expectErr && err == nil {
				t.Errorf("ParseShares() error %v, expectErr %v", err, tt.expectErr)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParseShares() got %v, want %v", got, tt.want)
			}
		})
	}
}

func generateRawShare(namespace namespace.ID, isMessageStart bool) (rawShare []byte) {
	infoByte, _ := NewInfoByte(appconsts.ShareVersion, isMessageStart)
	rawData := make([]byte, appconsts.ShareSize-len(rawShare))
	rand.Read(rawData)

	rawShare = append(rawShare, namespace...)
	rawShare = append(rawShare, byte(infoByte))
	rawShare = append(rawShare, rawData...)

	return rawShare
}

func generateRandomTxs(count, size int) types.Txs {
	txs := make(types.Txs, count)
	for i := 0; i < count; i++ {
		tx := make([]byte, size)
		_, err := rand.Read(tx)
		if err != nil {
			panic(err)
		}
		txs[i] = tx
	}
	return txs
}

func generateRandomMessageWithNamespace(namespace namespace.ID, size int) types.Message {
	msg := types.Message{
		NamespaceID: namespace,
		Data:        tmrand.Bytes(size),
	}
	return msg
}
