package shares

import (
	"testing"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/nmt/namespace"
	"github.com/stretchr/testify/assert"
)

func TestNamespacedPaddedShare(t *testing.T) {
	namespaceOne := namespace.ID{1, 1, 1, 1, 1, 1, 1, 1}

	want, _ := zeroPadIfNecessary([]byte{
		1, 1, 1, 1, 1, 1, 1, 1, // namespace ID
		1,          // info byte
		0, 0, 0, 0, // sequence len
	}, appconsts.ShareSize)

	got := NamespacedPaddedShare(namespaceOne).ToBytes()
	assert.Equal(t, want, got)
}

func TestNamespacedPaddedShares(t *testing.T) {
	namespaceOne := namespace.ID{1, 1, 1, 1, 1, 1, 1, 1}

	want, _ := zeroPadIfNecessary([]byte{
		1, 1, 1, 1, 1, 1, 1, 1, // namespace ID
		1,          // info byte
		0, 0, 0, 0, // sequence len
	}, appconsts.ShareSize)

	shares := NamespacedPaddedShares(namespaceOne, 2)
	for _, share := range shares {
		assert.Equal(t, want, share.ToBytes())
	}
}
