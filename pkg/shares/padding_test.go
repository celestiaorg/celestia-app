package shares

import (
	"testing"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/nmt/namespace"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var nsOne = namespace.ID{1, 1, 1, 1, 1, 1, 1, 1}

var nsOnePadding, _ = zeroPadIfNecessary([]byte{
	1, 1, 1, 1, 1, 1, 1, 1, // namespace ID
	1,          // info byte
	0, 0, 0, 0, // sequence len
}, appconsts.ShareSize)

var reservedPadding, _ = zeroPadIfNecessary([]byte{
	0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0xff, // namespace ID
	1,          // info byte
	0, 0, 0, 0, // sequence len
}, appconsts.ShareSize)

var tailPadding, _ = zeroPadIfNecessary([]byte{
	0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFE, // namespace ID
	1,          // info byte
	0, 0, 0, 0, // sequence len
}, appconsts.ShareSize)

func TestNamespacePaddingShare(t *testing.T) {
	got, err := NamespacePaddingShare(nsOne)
	require.NoError(t, err)
	assert.Equal(t, nsOnePadding, got.ToBytes())
}

func TestNamespacePaddingShares(t *testing.T) {
	shares, err := NamespacePaddingShares(nsOne, 2)
	require.NoError(t, err)
	for _, share := range shares {
		assert.Equal(t, nsOnePadding, share.ToBytes())
	}
}

func TestReservedPaddingShare(t *testing.T) {
	got, err := ReservedPaddingShare()
	require.NoError(t, err)
	assert.Equal(t, reservedPadding, got.ToBytes())
}

func TestReservedPaddingShares(t *testing.T) {
	shares, err := ReservedPaddingShares(2)
	require.NoError(t, err)
	for _, share := range shares {
		assert.Equal(t, reservedPadding, share.ToBytes())
	}
}

func TestTailPaddingShare(t *testing.T) {
	got, err := TailPaddingShare()
	require.NoError(t, err)
	assert.Equal(t, tailPadding, got.ToBytes())
}

func TestTailPaddingShares(t *testing.T) {
	shares, err := TailPaddingShares(2)
	require.NoError(t, err)
	for _, share := range shares {
		assert.Equal(t, tailPadding, share.ToBytes())
	}
}
