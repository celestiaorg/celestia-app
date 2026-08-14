package app_test

import (
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v10/app"
	testutil "github.com/celestiaorg/celestia-app/v10/test/util"
	"github.com/celestiaorg/celestia-app/v10/test/util/testfactory"
	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/stretchr/testify/require"
)

// TestProcessProposalSquareSizeOverflow verifies that ProcessProposal rejects a
// proposal whose declared SquareSize is congruent to the correct value modulo
// 2^63. The check in app/process_proposal.go compares
// `uint64(eds.Width()) != req.SquareSize*2`, and the multiplication wraps, so
// `S` and `S + 2^63` are indistinguishable to that comparison.
func TestProcessProposalSquareSizeOverflow(t *testing.T) {
	accounts := testfactory.GenerateAccounts(1)
	testApp, _ := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams(), accounts...)

	blockTime := time.Now()
	height := testApp.LastBlockHeight() + 1

	prepared, err := testApp.PrepareProposal(&abci.RequestPrepareProposal{
		Height: height,
		Time:   blockTime,
	})
	require.NoError(t, err)

	honest := prepared.SquareSize
	aliased := honest + (uint64(1) << 63)
	// Sanity check: the two values are indistinguishable after doubling.
	require.Equal(t, honest*2, aliased*2)

	t.Run("honest square size is accepted", func(t *testing.T) {
		resp, err := testApp.ProcessProposal(&abci.RequestProcessProposal{
			Time:         blockTime,
			Height:       height,
			Txs:          prepared.Txs,
			DataRootHash: prepared.DataRootHash,
			SquareSize:   honest,
		})
		require.NoError(t, err)
		require.Equal(t, abci.ResponseProcessProposal_ACCEPT, resp.Status)
	})

	t.Run("aliased square size is rejected", func(t *testing.T) {
		resp, err := testApp.ProcessProposal(&abci.RequestProcessProposal{
			Time:         blockTime,
			Height:       height,
			Txs:          prepared.Txs,
			DataRootHash: prepared.DataRootHash,
			SquareSize:   aliased,
		})
		require.NoError(t, err)
		require.Equal(t, abci.ResponseProcessProposal_REJECT, resp.Status,
			"ProcessProposal accepted SquareSize=%d when the real square size is %d", aliased, honest)
	})
}
