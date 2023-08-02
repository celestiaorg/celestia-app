package shares

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	appns "github.com/celestiaorg/celestia-app/pkg/namespace"
	"github.com/celestiaorg/celestia-app/test/util/testfactory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestShareSequenceRawData(t *testing.T) {
	type testCase struct {
		name          string
		shareSequence ShareSequence
		want          []byte
		wantErr       bool
	}
	blobNamespace := appns.RandomBlobNamespace()

	testCases := []testCase{
		{
			name: "empty share sequence",
			shareSequence: ShareSequence{
				Namespace: appns.TxNamespace,
				Shares:    []Share{},
			},
			want:    []byte{},
			wantErr: false,
		},
		{
			name: "one empty share",
			shareSequence: ShareSequence{
				Namespace: appns.TxNamespace,
				Shares: []Share{
					shareWithData(blobNamespace, true, 0, []byte{}),
				},
			},
			want:    []byte{},
			wantErr: false,
		},
		{
			name: "one share with one byte",
			shareSequence: ShareSequence{
				Namespace: appns.TxNamespace,
				Shares: []Share{
					shareWithData(blobNamespace, true, 1, []byte{0x0f}),
				},
			},
			want:    []byte{0xf},
			wantErr: false,
		},
		{
			name: "removes padding from last share",
			shareSequence: ShareSequence{
				Namespace: appns.TxNamespace,
				Shares: []Share{
					shareWithData(blobNamespace, true, appconsts.FirstSparseShareContentSize+1, bytes.Repeat([]byte{0xf}, appconsts.FirstSparseShareContentSize)),
					shareWithData(blobNamespace, false, 0, []byte{0x0f}),
				},
			},
			want:    bytes.Repeat([]byte{0xf}, appconsts.FirstSparseShareContentSize+1),
			wantErr: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.shareSequence.RawData()
			if tc.wantErr {
				assert.Error(t, err)
				return
			}
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestCompactSharesNeeded(t *testing.T) {
	type testCase struct {
		sequenceLen int
		want        int
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
		got := CompactSharesNeeded(tc.sequenceLen)
		assert.Equal(t, tc.want, got)
	}
}

func TestSparseSharesNeeded(t *testing.T) {
	type testCase struct {
		sequenceLen uint32
		want        int
	}
	testCases := []testCase{
		{0, 0},
		{1, 1},
		{2, 1},
		{appconsts.FirstSparseShareContentSize, 1},
		{appconsts.FirstSparseShareContentSize + 1, 2},
		{appconsts.FirstSparseShareContentSize + appconsts.ContinuationSparseShareContentSize, 2},
		{appconsts.FirstSparseShareContentSize + appconsts.ContinuationCompactShareContentSize*2, 3},
		{appconsts.FirstSparseShareContentSize + appconsts.ContinuationCompactShareContentSize*99, 100},
		{1000, 3},
		{10000, 21},
		{100000, 208},
	}
	for _, tc := range testCases {
		got := SparseSharesNeeded(tc.sequenceLen)
		assert.Equal(t, tc.want, got)
	}
}

func shareWithData(namespace appns.Namespace, isSequenceStart bool, sequenceLen uint32, data []byte) (rawShare Share) {
	infoByte, _ := NewInfoByte(appconsts.ShareVersionZero, isSequenceStart)
	rawShareBytes := make([]byte, 0, appconsts.ShareSize)
	rawShareBytes = append(rawShareBytes, namespace.Bytes()...)
	rawShareBytes = append(rawShareBytes, byte(infoByte))
	if isSequenceStart {
		sequenceLenBuf := make([]byte, appconsts.SequenceLenBytes)
		binary.BigEndian.PutUint32(sequenceLenBuf, sequenceLen)
		rawShareBytes = append(rawShareBytes, sequenceLenBuf...)
	}
	rawShareBytes = append(rawShareBytes, data...)

	return padShare(Share{data: rawShareBytes})
}

func Test_validSequenceLen(t *testing.T) {
	type testCase struct {
		name          string
		shareSequence ShareSequence
		wantErr       bool
	}

	tailPadding := ShareSequence{
		Namespace: appns.TailPaddingNamespace,
		Shares:    []Share{TailPaddingShare()},
	}

	ns1 := appns.MustNewV0(bytes.Repeat([]byte{0x1}, appns.NamespaceVersionZeroIDSize))
	share, err := NamespacePaddingShare(ns1, appconsts.ShareVersionZero)
	require.NoError(t, err)
	namespacePadding := ShareSequence{
		Namespace: ns1,
		Shares:    []Share{share},
	}

	reservedPadding := ShareSequence{
		Namespace: appns.ReservedPaddingNamespace,
		Shares:    []Share{ReservedPaddingShare()},
	}

	notSequenceStart := ShareSequence{
		Namespace: ns1,
		Shares: []Share{
			shareWithData(ns1, false, 0, []byte{0x0f}),
		},
	}

	testCases := []testCase{
		{
			name:          "empty share sequence",
			shareSequence: ShareSequence{},
			wantErr:       true,
		},
		{
			name:          "valid share sequence",
			shareSequence: generateValidShareSequence(t),
			wantErr:       false,
		},
		{
			name:          "tail padding",
			shareSequence: tailPadding,
			wantErr:       false,
		},
		{
			name:          "namespace padding",
			shareSequence: namespacePadding,
			wantErr:       false,
		},
		{
			name:          "reserved padding",
			shareSequence: reservedPadding,
			wantErr:       false,
		},
		{
			name:          "sequence length where first share is not sequence start",
			shareSequence: notSequenceStart,
			wantErr:       true, // error: "share sequence has 1 shares but needed 0 shares"
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.shareSequence.validSequenceLen()
			if tc.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
		})
	}
}

func generateValidShareSequence(t *testing.T) ShareSequence {
	css := NewCompactShareSplitter(appns.TxNamespace, appconsts.ShareVersionZero)
	txs := testfactory.GenerateRandomTxs(5, 200)
	for _, tx := range txs {
		err := css.WriteTx(tx)
		require.NoError(t, err)
	}
	shares, err := css.Export()
	require.NoError(t, err)

	return ShareSequence{
		Namespace: appns.TxNamespace,
		Shares:    shares,
	}
}

func FuzzValidSequenceLen(f *testing.F) {
	f.Fuzz(func(t *testing.T, rawData []byte, rawNamespace []byte) {
		share, err := NewShare(rawData)
		if err != nil {
			t.Skip()
		}

		ns, err := appns.From(rawNamespace)
		if err != nil {
			t.Skip()
		}

		shareSequence := ShareSequence{
			Namespace: ns,
			Shares:    []Share{*share},
		}

		// want := fmt.Errorf("share sequence has 1 shares but needed 0 shares")
		err = shareSequence.validSequenceLen()
		assert.NoError(t, err)
	})
}
