package shares

import (
	"bytes"
	"encoding/binary"
	"math/rand"
	"reflect"
	"testing"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	appns "github.com/celestiaorg/celestia-app/pkg/namespace"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	ns1 := appns.MustNewV0(bytes.Repeat([]byte{1}, appns.NamespaceVersionZeroIDSize))
	namespaceTwo := appns.MustNewV0(bytes.Repeat([]byte{1}, appns.NamespaceVersionZeroIDSize))

	txShares, _, _, err := SplitTxs(generateRandomTxs(2, 1000))
	require.NoError(t, err)
	txShareStart := txShares[0]
	txShareContinuation := txShares[1]

	blobOneShares, err := SplitBlobs(0, []uint32{}, []types.Blob{generateRandomBlobWithNamespace(ns1, 1000)}, false)
	if err != nil {
		t.Fatal(err)
	}
	blobOneStart := blobOneShares[0]
	blobOneContinuation := blobOneShares[1]

	blobTwoShares, err := SplitBlobs(0, []uint32{}, []types.Blob{generateRandomBlobWithNamespace(namespaceTwo, 1000)}, false)
	if err != nil {
		t.Fatal(err)
	}
	blobTwoStart := blobTwoShares[0]
	blobTwoContinuation := blobTwoShares[1]

	invalidShare := Share{data: append(generateRawShare(ns1, start, 1), []byte{0}...)} // invalidShare is now longer than the length of a valid share

	largeSequenceLen := 1000 // it takes more than one share to store a sequence of 1000 bytes
	oneShareWithTooLargeSequenceLen := generateRawShare(ns1, start, uint32(largeSequenceLen))

	shortSequenceLen := 0
	oneShareWithTooShortSequenceLen := generateRawShare(ns1, start, uint32(shortSequenceLen))

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
			[]ShareSequence{{Namespace: appns.TxNamespace, Shares: []Share{txShareStart}}},
			false,
		},
		{
			"two transaction shares",
			[]Share{txShareStart, txShareContinuation},
			[]ShareSequence{{Namespace: appns.TxNamespace, Shares: []Share{txShareStart, txShareContinuation}}},
			false,
		},
		{
			"one blob share",
			[]Share{blobOneStart},
			[]ShareSequence{{Namespace: ns1, Shares: []Share{blobOneStart}}},
			false,
		},
		{
			"two blob shares",
			[]Share{blobOneStart, blobOneContinuation},
			[]ShareSequence{{Namespace: ns1, Shares: []Share{blobOneStart, blobOneContinuation}}},
			false,
		},
		{
			"two blobs with two shares each",
			[]Share{blobOneStart, blobOneContinuation, blobTwoStart, blobTwoContinuation},
			[]ShareSequence{
				{Namespace: ns1, Shares: []Share{blobOneStart, blobOneContinuation}},
				{Namespace: namespaceTwo, Shares: []Share{blobTwoStart, blobTwoContinuation}},
			},
			false,
		},
		{
			"one transaction, one blob",
			[]Share{txShareStart, blobOneStart},
			[]ShareSequence{
				{Namespace: appns.TxNamespace, Shares: []Share{txShareStart}},
				{Namespace: ns1, Shares: []Share{blobOneStart}},
			},
			false,
		},
		{
			"one transaction, two blobs",
			[]Share{txShareStart, blobOneStart, blobTwoStart},
			[]ShareSequence{
				{Namespace: appns.TxNamespace, Shares: []Share{txShareStart}},
				{Namespace: ns1, Shares: []Share{blobOneStart}},
				{Namespace: namespaceTwo, Shares: []Share{blobTwoStart}},
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
			[]Share{{data: oneShareWithTooLargeSequenceLen}},
			[]ShareSequence{},
			true,
		},
		{
			"one share with too short sequence length",
			[]Share{{data: oneShareWithTooShortSequenceLen}},
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

func generateRawShare(namespace appns.Namespace, isSequenceStart bool, sequenceLen uint32) (rawShare []byte) {
	infoByte, _ := NewInfoByte(appconsts.ShareVersionZero, isSequenceStart)

	sequenceLenBuf := make([]byte, appconsts.SequenceLenBytes)
	binary.BigEndian.PutUint32(sequenceLenBuf, sequenceLen)

	rawShare = append(rawShare, namespace.Bytes()...)
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

func generateRandomBlobWithNamespace(namespace appns.Namespace, size int) types.Blob {
	blob := types.Blob{
		NamespaceVersion: namespace.Version,
		NamespaceID:      namespace.ID,
		Data:             tmrand.Bytes(size),
		ShareVersion:     appconsts.ShareVersionZero,
	}
	return blob
}
