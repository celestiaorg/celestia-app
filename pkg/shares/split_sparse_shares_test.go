package shares

import (
	"testing"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/nmt/namespace"
	"github.com/stretchr/testify/assert"
	coretypes "github.com/tendermint/tendermint/types"
)

func TestMarshalDelimitedBlob(t *testing.T) {
	type testCase struct {
		name string
		blob coretypes.Blob
		want []byte
	}

	testCases := []testCase{
		{
			name: "empty blob",
			blob: coretypes.Blob{},
			want: []byte{0x0, 0x0, 0x0, 0x0}, // sequence length
		},
		{
			name: "one byte blob",
			blob: coretypes.Blob{
				NamespaceID:  namespace.ID{0x1, 0x1, 0x1, 0x1, 0x1, 0x1, 0x1, 0x1},
				Data:         []byte{0xf},
				ShareVersion: appconsts.ShareVersionZero,
			},
			want: []byte{
				0x0, 0x0, 0x0, 0x1, // sequence length
				0xf, // data
			},
		},
		{
			name: "two byte blob",
			blob: coretypes.Blob{
				NamespaceID:  namespace.ID{0x1, 0x1, 0x1, 0x1, 0x1, 0x1, 0x1, 0x1},
				Data:         []byte{0xf, 0xf},
				ShareVersion: appconsts.ShareVersionZero,
			},
			want: []byte{
				0x0, 0x0, 0x0, 0x2, // sequence length
				0xf, 0xf, // data
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := MarshalDelimitedBlob(tc.blob)
			assert.Equal(t, tc.want, got)
		})
	}
}
