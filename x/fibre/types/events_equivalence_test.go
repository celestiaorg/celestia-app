package types

import (
	"math"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

// TestPayForFibreEventMatchesTypedEvent checks the hand-built event against the
// SDK's own conversion of the typed one. The event is consensus-visible, so a
// new field must fail here rather than change what indexers see.
func TestPayForFibreEventMatchesTypedEvent(t *testing.T) {
	cases := []struct {
		name           string
		signer         string
		namespace      []byte
		commitment     []byte
		validatorCount uint32
	}{
		{"ordinary", "celestia1abcdef", []byte{1, 2, 3}, []byte{4, 5, 6}, 100},
		{"nil bytes", "celestia1abcdef", nil, nil, 1},
		{"empty bytes", "celestia1abcdef", []byte{}, []byte{}, 0},
		{"empty signer", "", []byte{7}, []byte{8}, 2},
		{"html in signer", `a"<b>&'c`, []byte{9}, []byte{10}, 3},
		{"max count", "celestia1abcdef", []byte{11}, []byte{12}, math.MaxUint32},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			want, err := sdk.TypedEventToEvent(
				NewEventPayForFibre(tc.signer, tc.namespace, tc.commitment, tc.validatorCount))
			require.NoError(t, err)

			got := PayForFibreEvent(tc.signer, tc.namespace, tc.commitment, tc.validatorCount)
			require.Equal(t, want.Type, got.Type)
			require.Equal(t, len(want.Attributes), len(got.Attributes))
			for i := range want.Attributes {
				require.Equal(t, want.Attributes[i].Key, got.Attributes[i].Key, "attribute %d key", i)
				require.Equal(t, want.Attributes[i].Value, got.Attributes[i].Value, "attribute %d value", i)
			}
		})
	}
}
