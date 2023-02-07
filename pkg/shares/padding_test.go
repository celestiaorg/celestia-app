package shares

import (
	"testing"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/nmt/namespace"
	"github.com/stretchr/testify/assert"
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
	got := NamespacePaddingShare(nsOne).ToBytes()
	assert.Equal(t, nsOnePadding, got)
}

func TestNamespacePaddingShares(t *testing.T) {
	shares := NamespacePaddingShares(nsOne, 2)
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
