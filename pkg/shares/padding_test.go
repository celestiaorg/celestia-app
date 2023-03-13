package shares

import (
	"bytes"
	"testing"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/stretchr/testify/assert"
)

var namespaceOne = bytes.Repeat([]byte{1}, appconsts.NamespaceSize)

var nsOnePadding, _ = zeroPadIfNecessary(
	append(
		namespaceOne,
		[]byte{
			1,          // info byte
			0, 0, 0, 0, // sequence len
		}...,
	), appconsts.ShareSize)

var reservedPadding, _ = zeroPadIfNecessary(
	append(
		appconsts.ReservedPaddingNamespaceID,
		[]byte{
			1,          // info byte
			0, 0, 0, 0, // sequence len
		}...,
	), appconsts.ShareSize)

var tailPadding, _ = zeroPadIfNecessary(
	append(
		appconsts.TailPaddingNamespaceID,
		[]byte{
			1,          // info byte
			0, 0, 0, 0, // sequence len
		}...,
	), appconsts.ShareSize)

func TestNamespacePaddingShare(t *testing.T) {
	got := NamespacePaddingShare(namespaceOne).ToBytes()
	assert.Equal(t, nsOnePadding, got)
}

func TestNamespacePaddingShares(t *testing.T) {
	shares := NamespacePaddingShares(namespaceOne, 2)
	for _, share := range shares {
		assert.Equal(t, nsOnePadding, share.ToBytes())
	}
}

func TestReservedPaddingShare(t *testing.T) {
	got := ReservedPaddingShare().ToBytes()
	assert.Equal(t, reservedPadding, got)
}

func TestReservedPaddingShares(t *testing.T) {
	shares := ReservedPaddingShares(2)
	for _, share := range shares {
		assert.Equal(t, reservedPadding, share.ToBytes())
	}
}

func TestTailPaddingShare(t *testing.T) {
	got := TailPaddingShare().ToBytes()
	assert.Equal(t, tailPadding, got)
}

func TestTailPaddingShares(t *testing.T) {
	shares := TailPaddingShares(2)
	for _, share := range shares {
		assert.Equal(t, tailPadding, share.ToBytes())
	}
}
