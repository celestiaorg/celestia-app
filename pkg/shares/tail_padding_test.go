package shares

import (
	"testing"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/stretchr/testify/assert"
)

func TestTailPaddingShare(t *testing.T) {
	want, _ := zeroPadIfNecessary([]byte{
		0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFE, // namespace ID
		0x00, // info byte
	}, appconsts.ShareSize)
	got := TailPaddingShare().ToBytes()
	assert.Equal(t, want, got)
}

func TestTailPaddingShares(t *testing.T) {
	want, _ := zeroPadIfNecessary([]byte{
		0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFE, // namespace ID
		0x00, // info byte
	}, appconsts.ShareSize)

	shares := TailPaddingShares(2)
	for _, share := range shares {
		assert.Equal(t, want, share.ToBytes())
	}
}
