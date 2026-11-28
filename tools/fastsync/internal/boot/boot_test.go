package boot

import (
	"testing"

	"github.com/cometbft/cometbft/types"
	"github.com/stretchr/testify/require"
)

func TestCheckProposerRounds(t *testing.T) {
	vals, _ := types.RandValidatorSet(4, 10)
	later := vals.Copy()
	later.IncrementProposerPriority(1)
	hdr := &types.Header{Height: 7, ProposerAddress: later.GetProposer().Address}
	require.NotEqual(t, vals.GetProposer().Address, hdr.ProposerAddress)
	require.ErrorContains(t, checkProposer(vals, hdr, 0), "proposer")
	require.NoError(t, checkProposer(vals, hdr, 1), "proposed in round 1")
	hdr.ProposerAddress = vals.GetProposer().Address
	require.NoError(t, checkProposer(vals, hdr, 3), "re-proposed in a later round")
}
