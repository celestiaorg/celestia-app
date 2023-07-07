package shares

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	appns "github.com/celestiaorg/celestia-app/pkg/namespace"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestShareBuilderIsEmptyShare(t *testing.T) {
	type testCase struct {
		name    string
		builder *Builder
		data    []byte // input data
		want    bool
	}
	ns1 := appns.MustNewV0(bytes.Repeat([]byte{1}, appns.NamespaceVersionZeroIDSize))

	testCases := []testCase{
		{
			name:    "first compact share empty",
			builder: NewBuilder(appns.TxNamespace, appconsts.ShareVersionZero, true),
			data:    nil,
			want:    true,
		},
		{
			name:    "first compact share not empty",
			builder: NewBuilder(appns.TxNamespace, appconsts.ShareVersionZero, true),
			data:    []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
			want:    false,
		},
		{
			name:    "first sparse share empty",
			builder: NewBuilder(ns1, appconsts.ShareVersionZero, true),
			data:    nil,
			want:    true,
		},
		{
			name:    "first sparse share not empty",
			builder: NewBuilder(ns1, appconsts.ShareVersionZero, true),
			data:    []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
			want:    false,
		},
		{
			name:    "continues compact share empty",
			builder: NewBuilder(appns.TxNamespace, appconsts.ShareVersionZero, false),
			data:    nil,
			want:    true,
		},
		{
			name:    "continues compact share not empty",
			builder: NewBuilder(appns.TxNamespace, appconsts.ShareVersionZero, false),
			data:    []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
			want:    false,
		},
		{
			name:    "continues sparse share not empty",
			builder: NewBuilder(ns1, appconsts.ShareVersionZero, false),
			data:    []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
			want:    false,
		},
		{
			name:    "continues sparse share empty",
			builder: NewBuilder(ns1, appconsts.ShareVersionZero, false),
			data:    nil,
			want:    true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tc.builder.Init()
			require.NoError(t, err)
			tc.builder.AddData(tc.data)
			assert.Equal(t, tc.want, tc.builder.IsEmptyShare())
		})
	}
}

func TestShareBuilderWriteSequenceLen(t *testing.T) {
	type testCase struct {
		name    string
		builder *Builder
		wantLen uint32
		wantErr bool
	}
	ns1 := appns.MustNewV0(bytes.Repeat([]byte{1}, appns.NamespaceVersionZeroIDSize))

	testCases := []testCase{
		{
			name:    "first share",
			builder: NewBuilder(ns1, 1, true),
			wantLen: 10,
			wantErr: false,
		},
		{
			name:    "first share with long sequence",
			builder: NewBuilder(ns1, 1, true),
			wantLen: 323,
			wantErr: false,
		},
		{
			name:    "continuation sparse share",
			builder: NewBuilder(ns1, 1, false),
			wantLen: 10,
			wantErr: true,
		},
		{
			name:    "compact share",
			builder: NewBuilder(appns.TxNamespace, 1, true),
			wantLen: 10,
			wantErr: false,
		},
		{
			name:    "continuation compact share",
			builder: NewBuilder(ns1, 1, false),
			wantLen: 10,
			wantErr: true,
		},
		{
			name:    "nil builder",
			builder: &Builder{},
			wantLen: 10,
			wantErr: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tc.builder.Init()
			require.NoError(t, err)
			if err := tc.builder.WriteSequenceLen(tc.wantLen); tc.wantErr {
				assert.Error(t, err)
				return
			}

			tc.builder.ZeroPadIfNecessary()
			share, err := tc.builder.Build()
			require.NoError(t, err)

			length, err := share.SequenceLen()
			require.NoError(t, err)

			assert.Equal(t, tc.wantLen, length)
		})
	}
}

func TestShareBuilderAddData(t *testing.T) {
	type testCase struct {
		name    string
		builder *Builder
		data    []byte // input data
		want    []byte
	}
	ns1 := appns.MustNewV0(bytes.Repeat([]byte{1}, appns.NamespaceVersionZeroIDSize))

	testCases := []testCase{
		{
			name:    "small share",
			builder: NewBuilder(ns1, appconsts.ShareVersionZero, true),
			data:    []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
			want:    nil,
		},
		{
			name:    "exact fit first compact share",
			builder: NewBuilder(appns.TxNamespace, appconsts.ShareVersionZero, true),
			data:    bytes.Repeat([]byte{1}, appconsts.ShareSize-appconsts.NamespaceSize-appconsts.ShareInfoBytes-appconsts.CompactShareReservedBytes-appconsts.SequenceLenBytes),
			want:    nil,
		},
		{
			name:    "exact fit first sparse share",
			builder: NewBuilder(ns1, appconsts.ShareVersionZero, true),
			data:    bytes.Repeat([]byte{1}, appconsts.ShareSize-appconsts.NamespaceSize-appconsts.SequenceLenBytes-1 /*1 = info byte*/),
			want:    nil,
		},
		{
			name:    "exact fit continues compact share",
			builder: NewBuilder(appns.TxNamespace, appconsts.ShareVersionZero, false),
			data:    bytes.Repeat([]byte{1}, appconsts.ShareSize-appconsts.NamespaceSize-appconsts.CompactShareReservedBytes-1 /*1 = info byte*/),
			want:    nil,
		},
		{
			name:    "exact fit continues sparse share",
			builder: NewBuilder(ns1, appconsts.ShareVersionZero, false),
			data:    bytes.Repeat([]byte{1}, appconsts.ShareSize-appconsts.NamespaceSize-1 /*1 = info byte*/),
			want:    nil,
		},
		{
			name:    "oversize first compact share",
			builder: NewBuilder(appns.TxNamespace, appconsts.ShareVersionZero, true),
			data:    bytes.Repeat([]byte{1}, 1 /*1 extra byte*/ +appconsts.ShareSize-appconsts.NamespaceSize-appconsts.CompactShareReservedBytes-appconsts.SequenceLenBytes-1 /*1 = info byte*/),
			want:    []byte{1},
		},
		{
			name:    "oversize first sparse share",
			builder: NewBuilder(ns1, appconsts.ShareVersionZero, true),
			data:    bytes.Repeat([]byte{1}, 1 /*1 extra byte*/ +appconsts.ShareSize-appconsts.NamespaceSize-appconsts.SequenceLenBytes-1 /*1 = info byte*/),
			want:    []byte{1},
		},
		{
			name:    "oversize continues compact share",
			builder: NewBuilder(appns.TxNamespace, appconsts.ShareVersionZero, false),
			data:    bytes.Repeat([]byte{1}, 1 /*1 extra byte*/ +appconsts.ShareSize-appconsts.NamespaceSize-appconsts.CompactShareReservedBytes-1 /*1 = info byte*/),
			want:    []byte{1},
		},
		{
			name:    "oversize continues sparse share",
			builder: NewBuilder(ns1, appconsts.ShareVersionZero, false),
			data:    bytes.Repeat([]byte{1}, 1 /*1 extra byte*/ +appconsts.ShareSize-appconsts.NamespaceSize-1 /*1 = info byte*/),
			want:    []byte{1},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tc.builder.Init()
			require.NoError(t, err)

			got := tc.builder.AddData(tc.data)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestShareBuilderImportRawData(t *testing.T) {
	type testCase struct {
		name       string
		shareBytes []byte
		want       []byte
		wantErr    bool
	}
	ns1 := appns.MustNewV0(bytes.Repeat([]byte{1}, appns.NamespaceVersionZeroIDSize))

	firstSparseShare := append(ns1.Bytes(), []byte{
		1,           // info byte
		0, 0, 0, 10, // sequence len
		1, 2, 3, 4, 5, 6, 7, 8, 9, 10, // data
	}...)

	continuationSparseShare := append(ns1.Bytes(), []byte{
		0,                             // info byte
		1, 2, 3, 4, 5, 6, 7, 8, 9, 10, // data
	}...)

	firstCompactShare := append(appns.TxNamespace.Bytes(), []byte{
		1,           // info byte
		0, 0, 0, 10, // sequence len
		0, 0, 0, 15, // reserved bytes
		1, 2, 3, 4, 5, 6, 7, 8, 9, 10, // data
	}...)

	continuationCompactShare := append(appns.TxNamespace.Bytes(), []byte{
		0,          // info byte
		0, 0, 0, 0, // reserved bytes
		1, 2, 3, 4, 5, 6, 7, 8, 9, 10, // data
	}...)

	oversizedImport := append(
		append(
			ns1.Bytes(),
			[]byte{
				0,          // info byte
				0, 0, 0, 0, // reserved bytes
			}...), bytes.Repeat([]byte{1}, 513)...) // data

	testCases := []testCase{
		{
			name:       "first sparse share",
			shareBytes: firstSparseShare,
			want:       []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
		},
		{
			name:       "continuation sparse share",
			shareBytes: continuationSparseShare,
			want:       []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
		},
		{
			name:       "first compact share",
			shareBytes: firstCompactShare,
			want:       []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
		},
		{
			name:       "continuation compact share",
			shareBytes: continuationCompactShare,
			want:       []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
		},
		{
			name:       "oversized import",
			shareBytes: oversizedImport,
			wantErr:    true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			b := NewEmptyBuilder().ImportRawShare(tc.shareBytes)
			b.ZeroPadIfNecessary()
			builtShare, err := b.Build()
			if tc.wantErr {
				assert.Error(t, err)
				return
			}

			rawData, err := builtShare.RawData()
			if tc.wantErr {
				assert.Error(t, err)
				return
			}
			// Since rawData has padding, we need to use contains
			if !bytes.Contains(rawData, tc.want) {
				t.Errorf(fmt.Sprintf("%#v does not contain %#v", rawData, tc.want))
			}
		})
	}
}
