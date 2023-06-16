package shares

import (
	"bytes"
	"testing"

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
