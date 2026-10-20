package types

import (
	"math"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

// TestPayForFibreEventMatchesTypedEvent checks the hand-built event against the
// SDK's own conversion of the typed one. Events are not part of the results
// hash (celestia-core hashes only Code, Data, GasWanted and GasUsed), but
// indexers and clients read them, so a new field must fail here rather than
// change what they see.
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
			want, err := sdk.TypedEventToEvent(&EventPayForFibre{
				Signer:         tc.signer,
				Namespace:      tc.namespace,
				Commitment:     tc.commitment,
				ValidatorCount: tc.validatorCount,
			})
			require.NoError(t, err)

			// The whole event, so the attributes' Index flag is compared too.
			got := PayForFibreEvent(tc.signer, tc.namespace, tc.commitment, tc.validatorCount)
			require.Equal(t, want, got)
		})
	}
}
