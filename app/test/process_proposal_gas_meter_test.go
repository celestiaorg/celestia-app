package app_test

import (
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v10/app"
	"github.com/celestiaorg/celestia-app/v10/app/encoding"
	"github.com/celestiaorg/celestia-app/v10/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v10/pkg/user"
	testutil "github.com/celestiaorg/celestia-app/v10/test/util"
	"github.com/celestiaorg/celestia-app/v10/test/util/testfactory"
	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/stretchr/testify/require"
)

// TestProcessProposalAcceptsProposerBuiltBlockAtMinGas asserts that a block the
// proposer builds in PrepareProposal is accepted by ProcessProposal. The block
// level max square size read must not be metered against the leftover per
// transaction gas meter the ante loop leaves in ctx, otherwise a block whose
// last transaction has little gas remaining is rejected even though the proposer
// built it. See https://linear.app/celestia/issue/PROTOCO-2508.
//
// The test is self-calibrating: it finds the minimum gas limit at which the
// proposer keeps a plain MsgSend, then checks ProcessProposal accepts the block
// the proposer just built at that gas limit.
func TestProcessProposalAcceptsProposerBuiltBlockAtMinGas(t *testing.T) {
	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	accounts := testfactory.GenerateAccounts(2)
	testApp, kr := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams(), accounts...)
	infos := queryAccountInfo(testApp, accounts, kr)

	height := testApp.LastBlockHeight() + 1
	blockTime := time.Now()

	// buildTx creates a single MsgSend from accounts[0] at the current on-chain
	// sequence with the given gas limit. Nothing is committed between calls, so
	// the sequence stays constant and every tx is validly signed.
	buildTx := func(gasLimit uint64) []byte {
		fee := uint64(float64(gasLimit)*appconsts.DefaultMinGasPrice) + 1
		return []byte(testutil.SendTxWithManualSequence(
			t, enc.TxConfig, kr, accounts[0], accounts[1], 1, testutil.ChainID,
			infos[0].Sequence, infos[0].AccountNum,
			user.SetGasLimit(gasLimit), user.SetFee(fee),
		))
	}

	// prepareIncludes reports whether the proposer keeps the tx at this gas limit.
	prepareIncludes := func(gasLimit uint64) bool {
		resp, err := testApp.PrepareProposal(&abci.RequestPrepareProposal{
			Txs:    [][]byte{buildTx(gasLimit)},
			Height: height,
			Time:   blockTime,
		})
		require.NoError(t, err)
		return len(resp.Txs) == 1
	}

	// prepareThenProcess builds a block with the tx via PrepareProposal and feeds
	// that exact block to ProcessProposal, returning whether the tx was included
	// and how ProcessProposal voted.
	prepareThenProcess := func(gasLimit uint64) (bool, abci.ResponseProcessProposal_ProposalStatus) {
		presp, err := testApp.PrepareProposal(&abci.RequestPrepareProposal{
			Txs:    [][]byte{buildTx(gasLimit)},
			Height: height,
			Time:   blockTime,
		})
		require.NoError(t, err)

		prresp, err := testApp.ProcessProposal(&abci.RequestProcessProposal{
			Time:         blockTime,
			Height:       height,
			Txs:          presp.Txs,
			DataRootHash: presp.DataRootHash,
			SquareSize:   presp.SquareSize,
		})
		require.NoError(t, err)
		return len(presp.Txs) == 1, prresp.Status
	}

	// Binary search the minimum gas limit at which the proposer keeps the tx.
	// This is the proposer-side ante cost; ProcessProposal runs the same ante.
	lo, hi := uint64(20_000), uint64(200_000)
	require.False(t, prepareIncludes(lo), "sanity: tx must be dropped at a too-low gas limit")
	require.True(t, prepareIncludes(hi), "sanity: tx must be kept at a generous gas limit")
	for hi-lo > 1 {
		mid := (lo + hi) / 2
		if prepareIncludes(mid) {
			hi = mid
		} else {
			lo = mid
		}
	}
	minGasToBuild := hi

	included, status := prepareThenProcess(minGasToBuild)
	require.True(t, included, "proposer should keep the tx at its minimum viable gas limit")
	require.Equal(t, abci.ResponseProcessProposal_ACCEPT, status,
		"ProcessProposal must accept the block PrepareProposal built")
}
