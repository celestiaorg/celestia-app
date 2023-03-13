package shares

import (
	"encoding/binary"
	"math/rand"
	"reflect"
	"testing"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/nmt/namespace"
	"github.com/stretchr/testify/assert"
	tmrand "github.com/tendermint/tendermint/libs/rand"
	"github.com/tendermint/tendermint/types"
)

func TestParseShares(t *testing.T) {
	type testCase struct {
		name      string
		shares    []Share
		want      []ShareSequence
		expectErr bool
	}

	start := true
	blobOneNamespace := namespace.ID{1, 1, 1, 1, 1, 1, 1, 1}
	blobTwoNamespace := namespace.ID{2, 2, 2, 2, 2, 2, 2, 2}

	txShares, _, _ := SplitTxs(generateRandomTxs(2, 1000))
	txShareStart := txShares[0]
	txShareContinuation := txShares[1]

	blobOneShares, err := SplitBlobs(0, []uint32{}, []types.Blob{generateRandomBlobWithNamespace(blobOneNamespace, 1000)}, false)
	if err != nil {
		t.Fatal(err)
	}
	blobOneStart := blobOneShares[0]
	blobOneContinuation := blobOneShares[1]

	blobTwoShares, err := SplitBlobs(0, []uint32{}, []types.Blob{generateRandomBlobWithNamespace(blobTwoNamespace, 1000)}, false)
	if err != nil {
		t.Fatal(err)
	}
	blobTwoStart := blobTwoShares[0]
	blobTwoContinuation := blobTwoShares[1]

	invalidShareBytes := generateRawShare(blobOneNamespace, start, 1)
	invalidShareBytes = append(invalidShareBytes, []byte{0}...) // invalidShareBytes is now longer than the length of a valid share
	invalidShare := Share{data: invalidShareBytes}

	b := NewBuilder(blobOneNamespace, appconsts.ShareVersionZero, start)

	largeSequenceLen := 1000 // it takes more than one share to store a sequence of 1000 bytes
	oneShareWithTooLargeSequenceLenBytes := generateRawShare(blobOneNamespace, start, uint32(largeSequenceLen))
	b.ImportRawShare(oneShareWithTooLargeSequenceLenBytes)
	if err := b.WriteSequenceLen(uint32(largeSequenceLen)); err != nil {
		t.Fatal(err)
	}

	oneShareWithTooLargeSequenceLen, err := b.Build()
	if err != nil {
		t.Fatal(err)
	}

	shortSequenceLen := 0
	oneShareWithTooShortSequenceLenBytes := generateRawShare(blobOneNamespace, start, uint32(shortSequenceLen))
	b.ImportRawShare(oneShareWithTooShortSequenceLenBytes)
	if err := b.WriteSequenceLen(uint32(shortSequenceLen)); err != nil {
		t.Fatal(err)
	}
	oneShareWithTooShortSequenceLen, err := b.Build()
	if err != nil {
		t.Fatal(err)
	}

	tests := []testCase{
		{
			"empty",
			[]Share{},
			[]ShareSequence{},
			false,
		},
		{
			"one transaction share",
			[]Share{txShareStart},
			[]ShareSequence{{NamespaceID: appconsts.TxNamespaceID, Shares: []Share{txShareStart}}},
			false,
		},
		{
			"two transaction shares",
			[]Share{txShareStart, txShareContinuation},
			[]ShareSequence{{NamespaceID: appconsts.TxNamespaceID, Shares: []Share{txShareStart, txShareContinuation}}},
			false,
		},
		{
			"one blob share",
			[]Share{blobOneStart},
			[]ShareSequence{{NamespaceID: blobOneNamespace, Shares: []Share{blobOneStart}}},
			false,
		},
		{
			"two blob shares",
			[]Share{blobOneStart, blobOneContinuation},
			[]ShareSequence{{NamespaceID: blobOneNamespace, Shares: []Share{blobOneStart, blobOneContinuation}}},
			false,
		},
		{
			"two blobs with two shares each",
			[]Share{blobOneStart, blobOneContinuation, blobTwoStart, blobTwoContinuation},
			[]ShareSequence{
				{NamespaceID: blobOneNamespace, Shares: []Share{blobOneStart, blobOneContinuation}},
				{NamespaceID: blobTwoNamespace, Shares: []Share{blobTwoStart, blobTwoContinuation}},
			},
			false,
		},
		{
			"one transaction, one blob",
			[]Share{txShareStart, blobOneStart},
			[]ShareSequence{
				{NamespaceID: appconsts.TxNamespaceID, Shares: []Share{txShareStart}},
				{NamespaceID: blobOneNamespace, Shares: []Share{blobOneStart}},
			},
			false,
		},
		{
			"one transaction, two blobs",
			[]Share{txShareStart, blobOneStart, blobTwoStart},
			[]ShareSequence{
				{NamespaceID: appconsts.TxNamespaceID, Shares: []Share{txShareStart}},
				{NamespaceID: blobOneNamespace, Shares: []Share{blobOneStart}},
				{NamespaceID: blobTwoNamespace, Shares: []Share{blobTwoStart}},
			},
			false,
		},
		{
			"one share with invalid size",
			[]Share{invalidShare},
			[]ShareSequence{},
			true,
		},
		{
			"blob one start followed by blob two continuation",
			[]Share{blobOneStart, blobTwoContinuation},
			[]ShareSequence{},
			true,
		},
		{
			"one share with too large sequence length",
			[]Share{*oneShareWithTooLargeSequenceLen},
			[]ShareSequence{},
			true,
		},
		{
			"one share with too short sequence length",
			[]Share{*oneShareWithTooShortSequenceLen},
			[]ShareSequence{},
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseShares(tt.shares)
			if tt.expectErr {
				assert.Error(t, err)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParseShares() got %v, want %v", got, tt.want)
			}
		})
	}
}

func generateRawShare(namespace namespace.ID, isSequenceStart bool, sequenceLen uint32) (rawShare []byte) {
	infoByte, _ := NewInfoByte(appconsts.ShareVersionZero, isSequenceStart)

	sequenceLenBuf := make([]byte, appconsts.SequenceLenBytes)
	binary.BigEndian.PutUint32(sequenceLenBuf, sequenceLen)

	rawShare = append(rawShare, namespace...)
	rawShare = append(rawShare, byte(infoByte))
	rawShare = append(rawShare, sequenceLenBuf...)

	return padWithRandomBytes(rawShare)
}

func padWithRandomBytes(partialShare []byte) (paddedShare []byte) {
	paddedShare = make([]byte, appconsts.ShareSize)
	copy(paddedShare, partialShare)
	rand.Read(paddedShare[len(partialShare):])
	return paddedShare
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

func generateRandomBlobWithNamespace(namespace namespace.ID, size int) types.Blob {
	blob := types.Blob{
		NamespaceID: namespace,
		Data:        tmrand.Bytes(size),
	}
	return blob
}
