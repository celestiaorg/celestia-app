package shares

import (
	"math/rand"
	"reflect"
	"testing"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/nmt/namespace"
)

func TestParseShares(t *testing.T) {
	type testCase struct {
		name      string
		shares    [][]byte
		want      []ShareSequence
		expectErr bool
	}

	start := true
	continuation := false
	messageOneNamespace := namespace.ID{1, 1, 1, 1, 1, 1, 1, 1}
	messageTwoNamespace := namespace.ID{2, 2, 2, 2, 2, 2, 2, 2}

	transactionShareStart := generateRawShare(appconsts.TxNamespaceID, start)
	transactionShareContinuation := generateRawShare(appconsts.TxNamespaceID, continuation)

	evidenceShareStart := generateRawShare(appconsts.EvidenceNamespaceID, start)
	evidenceShareContinuation := generateRawShare(appconsts.EvidenceNamespaceID, continuation)

	messageOneStart := generateRawShare(messageOneNamespace, start)
	messageOneContinuation := generateRawShare(messageOneNamespace, continuation)

	messageTwoStart := generateRawShare(messageTwoNamespace, start)
	messageTwoContinuation := generateRawShare(messageTwoNamespace, continuation)

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
			"one evidence share",
			[][]byte{evidenceShareStart},
			[]ShareSequence{{NamespaceID: appconsts.EvidenceNamespaceID, Shares: []Share{evidenceShareStart}}},
			false,
		},
		{
			"two evidence shares",
			[][]byte{evidenceShareStart, evidenceShareContinuation},
			[]ShareSequence{{NamespaceID: appconsts.EvidenceNamespaceID, Shares: []Share{evidenceShareStart, evidenceShareContinuation}}},
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
			"one transaction, one evidence, one message",
			[][]byte{transactionShareStart, evidenceShareStart, messageOneStart},
			[]ShareSequence{
				{NamespaceID: appconsts.TxNamespaceID, Shares: []Share{transactionShareStart}},
				{NamespaceID: appconsts.EvidenceNamespaceID, Shares: []Share{evidenceShareStart}},
				{NamespaceID: messageOneNamespace, Shares: []Share{messageOneStart}},
			},
			false,
		},
		{
			"one transaction, one evidence, two messages",
			[][]byte{transactionShareStart, evidenceShareStart, messageOneStart, messageTwoStart},
			[]ShareSequence{
				{NamespaceID: appconsts.TxNamespaceID, Shares: []Share{transactionShareStart}},
				{NamespaceID: appconsts.EvidenceNamespaceID, Shares: []Share{evidenceShareStart}},
				{NamespaceID: messageOneNamespace, Shares: []Share{messageOneStart}},
				{NamespaceID: messageTwoStart, Shares: []Share{messageTwoStart}},
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
			if reflect.DeepEqual(got, tt.want) {
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
