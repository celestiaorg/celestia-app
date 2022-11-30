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
		shares    [][]byte
		want      []ShareSequence
		expectErr bool
	}

	start := true
	blobOneNamespace := namespace.ID{1, 1, 1, 1, 1, 1, 1, 1}
	blobTwoNamespace := namespace.ID{2, 2, 2, 2, 2, 2, 2, 2}

	transactionShares := SplitTxs(generateRandomTxs(2, 1000))
	transactionShareStart := transactionShares[0]
	transactionShareContinuation := transactionShares[1]

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

	invalidShare := generateRawShare(blobOneNamespace, start, 1)
	invalidShare = append(invalidShare, []byte{0}...) // invalidShare is now longer than the length of a valid share

	largeSequenceLength := 1000 // it takes more than one share to store a sequence of 1000 bytes
	oneShareWithTooLargeSequenceLength := generateRawShare(blobOneNamespace, start, uint64(largeSequenceLength))

	shortSequenceLength := 0
	oneShareWithTooShortSequenceLength := generateRawShare(blobOneNamespace, start, uint64(shortSequenceLength))

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
			"one blob share",
			[][]byte{blobOneStart},
			[]ShareSequence{{NamespaceID: blobOneNamespace, Shares: []Share{blobOneStart}}},
			false,
		},
		{
			"two blob shares",
			[][]byte{blobOneStart, blobOneContinuation},
			[]ShareSequence{{NamespaceID: blobOneNamespace, Shares: []Share{blobOneStart, blobOneContinuation}}},
			false,
		},
		{
			"two blobs with two shares each",
			[][]byte{blobOneStart, blobOneContinuation, blobTwoStart, blobTwoContinuation},
			[]ShareSequence{
				{NamespaceID: blobOneNamespace, Shares: []Share{blobOneStart, blobOneContinuation}},
				{NamespaceID: blobTwoNamespace, Shares: []Share{blobTwoStart, blobTwoContinuation}},
			},
			false,
		},
		{
			"one transaction, one blob",
			[][]byte{transactionShareStart, blobOneStart},
			[]ShareSequence{
				{NamespaceID: appconsts.TxNamespaceID, Shares: []Share{transactionShareStart}},
				{NamespaceID: blobOneNamespace, Shares: []Share{blobOneStart}},
			},
			false,
		},
		{
			"one transaction, two blobs",
			[][]byte{transactionShareStart, blobOneStart, blobTwoStart},
			[]ShareSequence{
				{NamespaceID: appconsts.TxNamespaceID, Shares: []Share{transactionShareStart}},
				{NamespaceID: blobOneNamespace, Shares: []Share{blobOneStart}},
				{NamespaceID: blobTwoNamespace, Shares: []Share{blobTwoStart}},
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
			"blob one start followed by blob two continuation",
			[][]byte{blobOneStart, blobTwoContinuation},
			[]ShareSequence{},
			true,
		},
		{
			"one share with too large sequence length",
			[][]byte{oneShareWithTooLargeSequenceLength},
			[]ShareSequence{},
			true,
		},
		{
			"one share with too short sequence length",
			[][]byte{oneShareWithTooShortSequenceLength},
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

func Test_compactSharesNeeded(t *testing.T) {
	type testCase struct {
		sequenceLength int
		want           int
	}
	testCases := []testCase{
		{0, 0},
		{1, 1},
		{2, 1},
		{appconsts.FirstCompactShareContentSize, 1},
		{appconsts.FirstCompactShareContentSize + 1, 2},
		{appconsts.FirstCompactShareContentSize + appconsts.ContinuationCompactShareContentSize, 2},
		{appconsts.FirstCompactShareContentSize + appconsts.ContinuationCompactShareContentSize*100, 101},
	}
	for _, tc := range testCases {
		got := compactSharesNeeded(tc.sequenceLength)
		assert.Equal(t, tc.want, got)
	}
}

func Test_sparseSharesNeeded(t *testing.T) {
	type testCase struct {
		sequenceLength int
		want           int
	}
	testCases := []testCase{
		{0, 0},
		{1, 1},
		{2, 1},
		{appconsts.SparseShareContentSize, 1},
		{appconsts.SparseShareContentSize + 1, 2},
		{appconsts.SparseShareContentSize * 2, 2},
		{appconsts.SparseShareContentSize*100 + 1, 101},
	}
	for _, tc := range testCases {
		got := sparseSharesNeeded(tc.sequenceLength)
		assert.Equal(t, tc.want, got)
	}
}

func generateRawShare(namespace namespace.ID, isSequenceStart bool, sequenceLength uint64) (rawShare []byte) {
	infoByte, _ := NewInfoByte(appconsts.ShareVersionZero, isSequenceStart)

	sequenceLengthVarint := make([]byte, binary.MaxVarintLen64)
	numBytesWritten := binary.PutUvarint(sequenceLengthVarint, sequenceLength)

	rawShare = append(rawShare, namespace...)
	rawShare = append(rawShare, byte(infoByte))
	rawShare = append(rawShare, sequenceLengthVarint[:numBytesWritten]...)

	return padWithRandomBytes(rawShare)
}

func padWithRandomBytes(partialShare Share) (paddedShare Share) {
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
