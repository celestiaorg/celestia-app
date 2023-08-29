package shares

import (
	"bytes"
	"testing"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	appns "github.com/celestiaorg/celestia-app/pkg/namespace"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
		appns.PrimaryReservedPaddingNamespace.Bytes(),
		[]byte{
			1,          // info byte
			0, 0, 0, 0, // sequence len
		}...,
	), appconsts.ShareSize)

var tailPadding, _ = zeroPadIfNecessary(
	append(
		appns.TailPaddingNamespace.Bytes(),
		[]byte{
			1,          // info byte
			0, 0, 0, 0, // sequence len
		}...,
	), appconsts.ShareSize)

func TestNamespacePaddingShare(t *testing.T) {
	got, err := NamespacePaddingShare(ns1, appconsts.ShareVersionZero)
	assert.NoError(t, err)
	assert.Equal(t, nsOnePadding, got.ToBytes())
}

func TestNamespacePaddingShares(t *testing.T) {
	shares, err := NamespacePaddingShares(ns1, appconsts.ShareVersionZero, 2)
	assert.NoError(t, err)
	for _, share := range shares {
		assert.Equal(t, nsOnePadding, share.ToBytes())
	}
}

func TestReservedPaddingShare(t *testing.T) {
	require.NotPanics(t, func() {
		got := ReservedPaddingShare()
		assert.Equal(t, reservedPadding, got.ToBytes())
	})
}

func TestReservedPaddingShares(t *testing.T) {
	require.NotPanics(t, func() {
		shares := ReservedPaddingShares(2)
		for _, share := range shares {
			assert.Equal(t, reservedPadding, share.ToBytes())
		}
	})
}

func TestTailPaddingShare(t *testing.T) {
	require.NotPanics(t, func() {
		got := TailPaddingShare()
		assert.Equal(t, tailPadding, got.ToBytes())
	})
}

func TestTailPaddingShares(t *testing.T) {
	require.NotPanics(t, func() {
		shares := TailPaddingShares(2)
		for _, share := range shares {
			assert.Equal(t, tailPadding, share.ToBytes())
		}
	})
}
