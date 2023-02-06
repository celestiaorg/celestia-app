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

func TestIsNamespacedPadded(t *testing.T) {
	type testCase struct {
		name    string
		share   Share
		want    bool
		wantErr bool
	}
	emptyShare := Share{}
	blobShare, _ := zeroPadIfNecessary([]byte{
		1, 1, 1, 1, 1, 1, 1, 1, // namespace ID
		1,          // info byte
		0, 0, 0, 1, // sequence len
		0xff, // data
	}, appconsts.ShareSize)

	testCases := []testCase{
		{
			name:  "namespaced padded share",
			share: NamespacedPaddedShare(namespace.ID{1, 1, 1, 1, 1, 1, 1, 1}),
			want:  true,
		},
		{
			name:    "empty share",
			share:   emptyShare,
			want:    false,
			wantErr: true,
		},
		{
			name:    "blob share",
			share:   blobShare,
			want:    false,
			wantErr: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := IsNamespacedPadded(tc.share)
			if tc.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}
