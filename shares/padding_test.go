package shares

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var ns1 = MustNewV0Namespace(bytes.Repeat([]byte{1}, NamespaceVersionZeroIDSize))

var nsOnePadding, _ = zeroPadIfNecessary(
	append(
		ns1.Bytes(),
		[]byte{
			1,          // info byte
			0, 0, 0, 0, // sequence len
		}...,
	), ShareSize)

var reservedPadding, _ = zeroPadIfNecessary(
	append(
		PrimaryReservedPaddingNamespace.Bytes(),
		[]byte{
			1,          // info byte
			0, 0, 0, 0, // sequence len
		}...,
	), ShareSize)

var tailPadding, _ = zeroPadIfNecessary(
	append(
		TailPaddingNamespace.Bytes(),
		[]byte{
			1,          // info byte
			0, 0, 0, 0, // sequence len
		}...,
	), ShareSize)

func TestNamespacePaddingShare(t *testing.T) {
	got, err := NamespacePaddingShare(ns1, ShareVersionZero)
	assert.NoError(t, err)
	assert.Equal(t, nsOnePadding, got.ToBytes())
}

func TestNamespacePaddingShares(t *testing.T) {
	shares, err := NamespacePaddingShares(ns1, ShareVersionZero, 2)
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
