//go:build !benchmarks

package app_test

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	coretypes "github.com/cometbft/cometbft/types"
	"github.com/stretchr/testify/require"
)

// TestProposalMessageCapsFitTxLimit guards that PrepareProposal can never build
// a block that ProcessProposal (and Core) reject for its transaction count.
// PrepareProposal keeps a tx only if it adds at least one message to one of the
// SDK, PFB or PayForFibre caps, so their sum bounds the kept tx count.
func TestProposalMessageCapsFitTxLimit(t *testing.T) {
	sum := appconsts.MaxSDKMessages + appconsts.MaxPFBMessages + appconsts.MaxPayForFibreMessages
	require.LessOrEqual(t, sum, coretypes.MaxBlockTxs)
}
