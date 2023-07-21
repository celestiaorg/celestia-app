package shares

import (
	"bytes"
	"testing"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	appns "github.com/celestiaorg/celestia-app/pkg/namespace"
	"github.com/stretchr/testify/assert"
	coretypes "github.com/tendermint/tendermint/types"
)

// TestSparseShareSplitter tests that the spare share splitter can split blobs
// with different namespaces.
func TestSparseShareSplitter(t *testing.T) {
	ns1 := appns.MustNewV0(bytes.Repeat([]byte{1}, appns.NamespaceVersionZeroIDSize))
	ns2 := appns.MustNewV0(bytes.Repeat([]byte{2}, appns.NamespaceVersionZeroIDSize))

	blob1 := coretypes.Blob{
		NamespaceVersion: ns1.Version,
		NamespaceID:      ns1.ID,
		ShareVersion:     0,
		Data:             []byte("data1"),
	}
	blob2 := coretypes.Blob{
		NamespaceVersion: ns2.Version,
		NamespaceID:      ns2.ID,
		ShareVersion:     0,
		Data:             []byte("data2"),
	}
	sss := NewSparseShareSplitter()

	err := sss.Write(blob1)
	assert.NoError(t, err)

	err = sss.Write(blob2)
	assert.NoError(t, err)

	got := sss.Export()
	assert.Len(t, got, 2)
}

func TestWriteNamespacePaddingShares(t *testing.T) {
	ns1 := appns.MustNewV0(bytes.Repeat([]byte{1}, appns.NamespaceVersionZeroIDSize))
	blob1 := newBlob(ns1, appconsts.ShareVersionZero)

	sss := NewSparseShareSplitter()

	err := sss.Write(blob1)
	assert.NoError(t, err)
	err = sss.WriteNamespacePaddingShares(1)
	assert.NoError(t, err)

	// got is expected to be [blob1, padding]
	got := sss.Export()
	assert.Len(t, got, 2)

	// verify that the second share is padding
	isPadding, err := got[1].IsPadding()
	assert.NoError(t, err)
	assert.True(t, isPadding)

	// verify that the padding share has the same share version as blob1
	info, err := got[1].InfoByte()
	assert.NoError(t, err)
	assert.Equal(t, info.Version(), appconsts.ShareVersionZero)
}

func newBlob(ns appns.Namespace, shareVersion uint8) coretypes.Blob {
	return coretypes.Blob{
		NamespaceVersion: ns.Version,
		NamespaceID:      ns.ID,
		ShareVersion:     shareVersion,
		Data:             []byte("data"),
	}
}
