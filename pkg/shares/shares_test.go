package shares

import (
	"bytes"
	"testing"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	appns "github.com/celestiaorg/celestia-app/pkg/namespace"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSequenceLen(t *testing.T) {
	type testCase struct {
		name    string
		share   Share
		wantLen uint32
		wantErr bool
	}
	sparseNamespaceID := bytes.Repeat([]byte{1}, appconsts.NamespaceSize)
	firstShare := append(sparseNamespaceID,
		[]byte{
			1,           // info byte
			0, 0, 0, 10, // sequence len
			1, 2, 3, 4, 5, 6, 7, 8, 9, 10, // data
		}...)
	firstShareWithLongSequence := append(sparseNamespaceID,
		[]byte{
			1,           // info byte
			0, 0, 1, 67, // sequence len
		}...)
	continuationShare := append(sparseNamespaceID,
		[]byte{
			0, // info byte
		}...)
	compactShare := append(appns.TxNamespace.Bytes(),
		[]byte{
			1,           // info byte
			0, 0, 0, 10, // sequence len
		}...)
	noInfoByte := appns.TxNamespace.Bytes()
	noSequenceLen := append(appns.TxNamespace.Bytes(),
		[]byte{
			1, // info byte
		}...)
	testCases := []testCase{
		{
			name:    "first share",
			share:   Share{data: firstShare},
			wantLen: 10,
			wantErr: false,
		},
		{
			name:    "first share with long sequence",
			share:   Share{data: firstShareWithLongSequence},
			wantLen: 323,
			wantErr: false,
		},
		{
			name:    "continuation share",
			share:   Share{data: continuationShare},
			wantLen: 0,
			wantErr: false,
		},
		{
			name:    "compact share",
			share:   Share{data: compactShare},
			wantLen: 10,
			wantErr: false,
		},
		{
			name:    "no info byte returns error",
			share:   Share{data: noInfoByte},
			wantLen: 0,
			wantErr: true,
		},
		{
			name:    "no sequence len returns error",
			share:   Share{data: noSequenceLen},
			wantLen: 0,
			wantErr: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			length, err := tc.share.SequenceLen()

			if tc.wantErr {
				assert.Error(t, err)
				return
			}
			if tc.wantLen != length {
				t.Errorf("want %d, got %d", tc.wantLen, length)
			}
		})
	}
}

func TestRawData(t *testing.T) {
	type testCase struct {
		name    string
		share   Share
		want    []byte
		wantErr bool
	}
	sparseNamespaceID := appns.MustNewV0(bytes.Repeat([]byte{0x1}, appns.NamespaceVersionZeroIDSize))
	firstSparseShare := append(
		sparseNamespaceID.Bytes(),
		[]byte{
			1,           // info byte
			0, 0, 0, 10, // sequence len
			1, 2, 3, 4, 5, 6, 7, 8, 9, 10, // data
		}...)
	continuationSparseShare := append(
		sparseNamespaceID.Bytes(),
		[]byte{
			0,                             // info byte
			1, 2, 3, 4, 5, 6, 7, 8, 9, 10, // data
		}...)
	firstCompactShare := append(appns.TxNamespace.Bytes(),
		[]byte{
			1,           // info byte
			0, 0, 0, 10, // sequence len
			0, 0, 0, 15, // reserved bytes
			1, 2, 3, 4, 5, 6, 7, 8, 9, 10, // data
		}...)
	continuationCompactShare := append(appns.TxNamespace.Bytes(),
		[]byte{
			0,          // info byte
			0, 0, 0, 0, // reserved bytes
			1, 2, 3, 4, 5, 6, 7, 8, 9, 10, // data
		}...)
	noSequenceLen := append(appns.TxNamespace.Bytes(),
		[]byte{
			1, // info byte
		}...)
	notEnoughSequenceLenBytes := append(appns.TxNamespace.Bytes(),
		[]byte{
			1,        // info byte
			0, 0, 10, // sequence len
		}...)
	testCases := []testCase{
		{
			name:  "first sparse share",
			share: Share{data: firstSparseShare},
			want:  []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
		},
		{
			name:  "continuation sparse share",
			share: Share{data: continuationSparseShare},
			want:  []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
		},
		{
			name:  "first compact share",
			share: Share{data: firstCompactShare},
			want:  []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
		},
		{
			name:  "continuation compact share",
			share: Share{data: continuationCompactShare},
			want:  []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
		},
		{
			name:    "no sequence len returns error",
			share:   Share{data: noSequenceLen},
			wantErr: true,
		},
		{
			name:    "not enough sequence len bytes returns error",
			share:   Share{data: notEnoughSequenceLenBytes},
			wantErr: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			rawData, err := tc.share.RawData()
			if tc.wantErr {
				assert.Error(t, err)
				return
			}
			assert.Equal(t, tc.want, rawData)
		})
	}
}

func TestIsCompactShare(t *testing.T) {
	type testCase struct {
		name  string
		share Share
		want  bool
	}

	ns1 := appns.MustNewV0(bytes.Repeat([]byte{1}, appns.NamespaceVersionZeroIDSize))
	txShare, _ := zeroPadIfNecessary(appns.TxNamespace.Bytes(), appconsts.ShareSize)
	pfbTxShare, _ := zeroPadIfNecessary(appns.PayForBlobNamespace.Bytes(), appconsts.ShareSize)
	blobShare, _ := zeroPadIfNecessary(ns1.Bytes(), appconsts.ShareSize)

	testCases := []testCase{
		{
			name:  "tx share",
			share: Share{data: txShare},
			want:  true,
		},
		{
			name:  "pfb tx share",
			share: Share{data: pfbTxShare},
			want:  true,
		},
		{
			name:  "blob share",
			share: Share{data: blobShare},
			want:  false,
		},
	}

	for _, tc := range testCases {
		got, err := tc.share.IsCompactShare()
		assert.NoError(t, err)
		assert.Equal(t, tc.want, got)
	}
}

func TestIsPadding(t *testing.T) {
	type testCase struct {
		name    string
		share   Share
		want    bool
		wantErr bool
	}
	emptyShare := Share{}
	blobShare, _ := zeroPadIfNecessary(
		append(
			ns1.Bytes(),
			[]byte{
				1,          // info byte
				0, 0, 0, 1, // sequence len
				0xff, // data
			}...,
		),
		appconsts.ShareSize)

	nsPadding, err := NamespacePaddingShare(ns1)
	require.NoError(t, err)

	testCases := []testCase{
		{
			name:    "empty share",
			share:   emptyShare,
			wantErr: true,
		},
		{
			name:  "blob share",
			share: Share{data: blobShare},
			want:  false,
		},
		{
			name:  "namespace padding",
			share: nsPadding,
			want:  true,
		},
		{
			name:  "tail padding",
			share: TailPaddingShare(),
			want:  true,
		},
		{
			name:  "reserved padding",
			share: ReservedPaddingShare(),
			want:  true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.share.IsPadding()
			if tc.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}
