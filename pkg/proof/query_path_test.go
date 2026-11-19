package proof_test

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v11/pkg/proof"
	"github.com/stretchr/testify/require"
)

func TestIsProofQueryPath(t *testing.T) {
	testCases := []struct {
		path string
		want bool
	}{
		{"custom/txInclusionProof/0", true},
		{"custom/shareInclusionProof/0/1", true},
		{"/custom/txInclusionProof/0", true},
		{"/custom/shareInclusionProof/0/1", true},
		{"custom/txInclusionProof", true},
		{"custom/shareInclusionProof", true},
		{"custom/txInclusionProof/", true},
		{"", false},
		{"/", false},
		{"custom", false},
		{"custom/", false},
		{"custom/bank/balance", false},
		{"store/bank/key", false},
		{"app/simulate", false},
		{"txInclusionProof/0", false},
		{"custom/TxInclusionProof/0", false},
		{"/cosmos.bank.v1beta1.Query/Balance", false},
	}
	for _, tc := range testCases {
		t.Run(tc.path, func(t *testing.T) {
			require.Equal(t, tc.want, proof.IsProofQueryPath(tc.path))
		})
	}
}
