package shares

import (
	"bytes"
	"testing"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	appns "github.com/celestiaorg/celestia-app/pkg/namespace"
	"github.com/stretchr/testify/assert"
)

var ns1 = appns.MustNewV0(bytes.Repeat([]byte{1}, appns.NamespaceVersionZeroIDSize))

var nsOnePadding, _ = zeroPadIfNecessary(
	append(
		ns1.Bytes(),
		[]byte{
			1,          // info byte
			0, 0, 0, 0, // sequence len
		}...,
	), appconsts.ShareSize)

var reservedPadding, _ = zeroPadIfNecessary(
	append(
		appns.ReservedPaddingNamespaceID.Bytes(),
		[]byte{
			1,          // info byte
			0, 0, 0, 0, // sequence len
		}...,
	), appconsts.ShareSize)

var tailPadding, _ = zeroPadIfNecessary(
	append(
		appns.TailPaddingNamespaceID.Bytes(),
		[]byte{
			1,          // info byte
			0, 0, 0, 0, // sequence len
		}...,
	), appconsts.ShareSize)

func TestNamespacePaddingShare(t *testing.T) {
	got := NamespacePaddingShare(ns1).ToBytes()
	assert.Equal(t, nsOnePadding, got)
}

func TestNamespacePaddingShares(t *testing.T) {
	shares := NamespacePaddingShares(ns1, 2)
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
