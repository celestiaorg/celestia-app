package proof_test

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v9/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v9/pkg/proof"
	abci "github.com/cometbft/cometbft/abci/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

// TestQueryShareInclusionProofRejectsOversizedRequest verifies that
// QueryShareInclusionProof rejects req.Data larger than the maximum legitimate
// block payload so that callers cannot trigger an unbounded square.Construct
// invocation via /abci_query.
func TestQueryShareInclusionProofRejectsOversizedRequest(t *testing.T) {
	// One byte over the upper bound on max bytes a legitimate block can carry.
	oversized := make([]byte, appconsts.DefaultUpperBoundMaxBytes+1)

	rawProof, err := proof.QueryShareInclusionProof(
		sdk.Context{},
		[]string{"0", "1"},
		&abci.RequestQuery{Data: oversized},
	)
	require.Error(t, err)
	require.Contains(t, err.Error(), "too large")
	require.Empty(t, rawProof)
}

// TestQueryTxInclusionProofRejectsOversizedRequest verifies that
// QueryTxInclusionProof rejects req.Data larger than the maximum legitimate
// block payload so that callers cannot trigger an unbounded square.Construct
// invocation via /abci_query.
func TestQueryTxInclusionProofRejectsOversizedRequest(t *testing.T) {
	oversized := make([]byte, appconsts.DefaultUpperBoundMaxBytes+1)

	rawProof, err := proof.QueryTxInclusionProof(
		sdk.Context{},
		[]string{"0"},
		&abci.RequestQuery{Data: oversized},
	)
	require.Error(t, err)
	require.Contains(t, err.Error(), "too large")
	require.Empty(t, rawProof)
}
