package shares

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/pkg/blob"
	"github.com/celestiaorg/celestia-app/pkg/namespace"
	"github.com/celestiaorg/celestia-app/test/util/testfactory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_zeroPadIfNecessary(t *testing.T) {
	type args struct {
		share []byte
		width int
	}
	tests := []struct {
		name               string
		args               args
		wantPadded         []byte
		wantBytesOfPadding int
	}{
		{"pad", args{[]byte{1, 2, 3}, 6}, []byte{1, 2, 3, 0, 0, 0}, 3},
		{"not necessary (equal to shareSize)", args{[]byte{1, 2, 3}, 3}, []byte{1, 2, 3}, 0},
		{"not necessary (greater shareSize)", args{[]byte{1, 2, 3}, 2}, []byte{1, 2, 3}, 0},
	}
	for _, tt := range tests {
		tt := tt // stupid scopelint :-/
		t.Run(tt.name, func(t *testing.T) {
			gotPadded, gotBytesOfPadding := zeroPadIfNecessary(tt.args.share, tt.args.width)
			if !reflect.DeepEqual(gotPadded, tt.wantPadded) {
				t.Errorf("zeroPadIfNecessary gotPadded %v, wantPadded %v", gotPadded, tt.wantPadded)
			}
			if gotBytesOfPadding != tt.wantBytesOfPadding {
				t.Errorf("zeroPadIfNecessary gotBytesOfPadding %v, wantBytesOfPadding %v", gotBytesOfPadding, tt.wantBytesOfPadding)
			}
		})
	}
}

func TestParseDelimiter(t *testing.T) {
	for i := uint64(0); i < 100; i++ {
		tx := testfactory.GenerateRandomTxs(1, int(i))[0]
		input, err := MarshalDelimitedTx(tx)
		if err != nil {
			panic(err)
		}
		res, txLen, err := ParseDelimiter(input)
		if err != nil {
			panic(err)
		}
		assert.Equal(t, i, txLen)
		assert.Equal(t, []byte(tx), res)
	}
}

func TestShareIndexFromBlobIndex(t *testing.T) {
	// Test cases
	testCases := []struct {
		name      string
		blobSize  int
		blobIndex int
	}{
		{name: "first byte (expect 0)", blobSize: appconsts.FirstSparseShareContentSize, blobIndex: 0},
		{name: "last byte first share (expect 0)", blobSize: appconsts.FirstSparseShareContentSize + 1, blobIndex: appconsts.FirstSparseShareContentSize},
		{name: "last byte second share (expect 1)", blobSize: appconsts.FirstSparseShareContentSize + appconsts.ContinuationSparseShareContentSize, blobIndex: appconsts.FirstSparseShareContentSize},
		{name: "first byte third share (expect 2)", blobSize: appconsts.FirstSparseShareContentSize + 2*appconsts.ContinuationSparseShareContentSize, blobIndex: appconsts.FirstSparseShareContentSize + appconsts.ContinuationSparseShareContentSize},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// create a blob and namespace consisting of all 1s so that we can
			// easily find the 2 that is inserted later
			insertByte := byte(2)
			d := bytes.Repeat([]byte{1}, tc.blobSize)
			d[tc.blobIndex] = insertByte

			prefix := bytes.Repeat([]byte{0}, namespace.NamespaceVersionZeroPrefixSize)
			id := append(prefix, bytes.Repeat([]byte{1}, namespace.NamespaceVersionZeroIDSize)...)
			ns, err := namespace.New(namespace.NamespaceVersionZero, id)
			require.NoError(t, err)

			b := blob.Blob{
				NamespaceId:      ns.ID,
				Data:             d,
				ShareVersion:     uint32(appconsts.ShareVersionZero),
				NamespaceVersion: uint32(ns.Version),
			}

			shares, err := SplitBlobs(&b)
			require.NoError(t, err)

			shareIndex := ShareIndex(tc.blobSize, tc.blobIndex)

			s := shares[shareIndex]

			assert.Contains(t, s.data, insertByte, tc.name)
		})
	}
}
