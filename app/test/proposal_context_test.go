package app_test

import (
	"math"
	"testing"
	"time"

	storetypes "cosmossdk.io/store/types"
	"github.com/celestiaorg/celestia-app/v10/app"
	"github.com/celestiaorg/celestia-app/v10/pkg/appconsts"
	testutil "github.com/celestiaorg/celestia-app/v10/test/util"
	"github.com/celestiaorg/celestia-app/v10/test/util/testfactory"
	fibretypes "github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	abci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

// TestNewProposalContext checks that a proposal context carries the header and
// consensus params, has unlimited gas meters, and isolates its writes.
func TestNewProposalContext(t *testing.T) {
	accounts := []string{"sender", "recipient"}
	testApp, kr := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams(), accounts...)
	sender := testfactory.GetAddress(kr, accounts[0])
	recipient := testfactory.GetAddress(kr, accounts[1])
	header := cmtproto.Header{
		ChainID: testutil.ChainID,
		Height:  testApp.LastBlockHeight() + 1,
		Time:    testutil.GenesisTime.Add(time.Hour),
	}
	ctx := testApp.NewProposalContext(header)

	t.Run("retains header and consensus params", func(t *testing.T) {
		require.Equal(t, header, ctx.BlockHeader())
		require.Equal(t, header.ChainID, ctx.ChainID())
		require.Equal(t, header.Height, ctx.BlockHeight())
		require.True(t, header.Time.Equal(ctx.BlockTime()))
		params := ctx.ConsensusParams()
		require.Equal(t, app.DefaultConsensusParams().Block.MaxBytes, params.Block.MaxBytes)
		require.Equal(t, app.DefaultConsensusParams().Version.App, params.Version.App)
	})

	t.Run("gas meters are unlimited", func(t *testing.T) {
		for _, meter := range []storetypes.GasMeter{ctx.GasMeter(), ctx.BlockGasMeter()} {
			require.Equal(t, uint64(math.MaxUint64), meter.Limit())
			require.NotPanics(t, func() { meter.ConsumeGas(math.MaxUint64/2, "test") })
			require.False(t, meter.IsOutOfGas())
		}
	})

	t.Run("writes stay on the branch", func(t *testing.T) {
		balanceBefore := testApp.BankKeeper.GetBalance(testApp.NewProposalContext(header), sender, appconsts.BondDenom)
		coins := sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, 1))

		branch := testApp.NewProposalContext(header)
		require.NoError(t, testApp.BankKeeper.SendCoins(branch, sender, recipient, coins))
		require.Equal(t, balanceBefore.Sub(coins[0]), testApp.BankKeeper.GetBalance(branch, sender, appconsts.BondDenom))

		require.Equal(t, balanceBefore, testApp.BankKeeper.GetBalance(testApp.NewProposalContext(header), sender, appconsts.BondDenom))
		require.Equal(t, balanceBefore, testApp.BankKeeper.GetBalance(uncachedContext(testApp), sender, appconsts.BondDenom))
	})
}

// TestProposalBeginBlockParity checks that ProcessProposal does not commit the
// fibre BeginBlocker effects, and that FinalizeBlock commits the same effects
// as a proposal branch. TestProcessProposalMaturingWithdrawalThenPayForFibre
// checks that ProcessProposal applies them before txs.
func TestProposalBeginBlockParity(t *testing.T) {
	accounts := []string{"owner"}
	testApp, kr := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams(), accounts...)
	owner := testfactory.GetAddress(kr, accounts[0]).String()
	seedMaturedWithdrawal(t, testApp, testfactory.GetAddress(kr, accounts[0]), 100, 40)

	req := processProposalRequest(t, testApp, nil)
	header := cmtproto.Header{ChainID: testutil.ChainID, Height: req.Height, Time: req.Time}
	type fibreState struct {
		escrow      fibretypes.EscrowAccount
		withdrawals int
		floor       time.Time
	}
	read := func(ctx sdk.Context) fibreState {
		escrow, found := testApp.FibreKeeper.GetEscrowAccount(ctx, owner)
		require.True(t, found)
		floor, err := testApp.FibreKeeper.GetPromiseFreshnessFloor(ctx)
		require.NoError(t, err)
		return fibreState{escrow, len(testApp.FibreKeeper.GetWithdrawalsBySigner(ctx, owner)), floor}
	}
	before := read(testApp.NewProposalContext(header))
	require.Equal(t, 1, before.withdrawals)

	branch := testApp.NewProposalContext(header)
	require.NoError(t, testApp.FibreKeeper.BeginBlocker(branch))
	expected := read(branch)
	require.Zero(t, expected.withdrawals)
	require.NotEqual(t, before.escrow, expected.escrow)
	require.True(t, expected.floor.After(before.floor))

	resp, err := testApp.ProcessProposal(req)
	require.NoError(t, err)
	require.Equal(t, abci.ResponseProcessProposal_ACCEPT, resp.Status)
	require.Equal(t, before, read(testApp.NewProposalContext(header)))

	_, err = testApp.FinalizeBlock(&abci.RequestFinalizeBlock{
		Height: req.Height,
		Time:   req.Time,
		Hash:   testApp.LastCommitID().Hash,
	})
	require.NoError(t, err)
	_, err = testApp.Commit()
	require.NoError(t, err)
	require.Equal(t, expected, read(testApp.NewProposalContext(header)))
}
