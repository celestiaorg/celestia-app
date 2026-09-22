package app_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v10/app"
	"github.com/celestiaorg/celestia-app/v10/app/encoding"
	"github.com/celestiaorg/celestia-app/v10/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v10/pkg/user"
	testutil "github.com/celestiaorg/celestia-app/v10/test/util"
	"github.com/celestiaorg/celestia-app/v10/test/util/testfactory"
	fibretypes "github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	abci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"
)

// maturingWithdrawalCounts spans the common case (0) to a worst-case burst of
// many withdrawals maturing in one block, isolating the BeginBlocker cost.
var maturingWithdrawalCounts = []int{0, 100, 1000, 10000}

// BenchmarkProcessProposalMaturingWithdrawals measures ProcessProposal on an
// empty block while N withdrawals mature at block start. N=0 is the baseline,
// where the added BeginBlocker only opens empty iterators.
func BenchmarkProcessProposalMaturingWithdrawals(b *testing.B) {
	for _, n := range maturingWithdrawalCounts {
		b.Run(fmt.Sprintf("withdrawals=%d", n), func(b *testing.B) {
			accounts := testfactory.GenerateAccounts(1)
			testApp, kr := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams(), accounts...)
			seedManyMaturedWithdrawals(b, testApp, testfactory.GetAddress(kr, accounts[0]), n)
			req := processProposalRequest(b, testApp, [][]byte{})

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				resp, err := testApp.ProcessProposal(req)
				require.NoError(b, err)
				require.Equal(b, abci.ResponseProcessProposal_ACCEPT, resp.Status)
			}
		})
	}
}

// BenchmarkPrepareProposalMaturingWithdrawals is the proposer-side counterpart.
func BenchmarkPrepareProposalMaturingWithdrawals(b *testing.B) {
	for _, n := range maturingWithdrawalCounts {
		b.Run(fmt.Sprintf("withdrawals=%d", n), func(b *testing.B) {
			accounts := testfactory.GenerateAccounts(1)
			testApp, kr := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams(), accounts...)
			seedManyMaturedWithdrawals(b, testApp, testfactory.GetAddress(kr, accounts[0]), n)
			height := testApp.LastBlockHeight() + 1

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, err := testApp.PrepareProposal(&abci.RequestPrepareProposal{
					Txs:    [][]byte{},
					Height: height,
					Time:   time.Now(),
				})
				require.NoError(b, err)
			}
		})
	}
}

// BenchmarkProcessProposalUnderLoad measures ProcessProposal on a full 512-tx
// block, showing the added BeginBlocker cost as a fraction of a busy block.
func BenchmarkProcessProposalUnderLoad(b *testing.B) {
	const blockTxs = 512
	for _, n := range []int{0, 50, 200} {
		b.Run(fmt.Sprintf("txs=%d/withdrawals=%d", blockTxs, n), func(b *testing.B) {
			enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
			accounts := testfactory.GenerateAccounts(1)
			testApp, kr := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams(), accounts...)
			infos := queryAccountInfo(testApp, accounts, kr)
			signer, err := user.NewSigner(kr, enc.TxConfig, testutil.ChainID, user.NewAccount(accounts[0], infos[0].AccountNum, infos[0].Sequence))
			require.NoError(b, err)

			seedManyMaturedWithdrawals(b, testApp, testfactory.GetAddress(kr, accounts[0]), n)
			txs := benchSendTxs(b, signer, accounts[0], testfactory.GetAddress(kr, accounts[0]).String(), blockTxs)
			req := processProposalRequest(b, testApp, txs)

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				resp, err := testApp.ProcessProposal(req)
				require.NoError(b, err)
				require.Equal(b, abci.ResponseProcessProposal_ACCEPT, resp.Status)
			}
		})
	}
}

// benchSendTxs builds n sequential self-transfer MsgSend txs from the account.
func benchSendTxs(tb testing.TB, signer *user.Signer, account, fromAddr string, n int) [][]byte {
	tb.Helper()
	txs := make([][]byte, n)
	for i := range n {
		msg := &banktypes.MsgSend{
			FromAddress: fromAddr,
			ToAddress:   fromAddr,
			Amount:      sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, 1)),
		}
		txBytes, _, err := signer.CreateTx([]sdk.Msg{msg}, user.SetGasLimit(100_000), user.SetFee(10_000))
		require.NoError(tb, err)
		txs[i] = txBytes
		require.NoError(tb, signer.IncrementSequence(account))
	}
	return txs
}

// seedManyMaturedWithdrawals seeds n already-available withdrawals of 1 utia
// each on the owner's escrow, funding the module so the next BeginBlocker can
// pay them all out.
func seedManyMaturedWithdrawals(tb testing.TB, testApp *app.App, owner sdk.AccAddress, n int) {
	tb.Helper()
	if n == 0 {
		return
	}
	ctx := testApp.NewUncachedContext(false, cmtproto.Header{
		ChainID: testutil.ChainID,
		Height:  testApp.LastBlockHeight(),
		Time:    time.Now(),
	})
	total := sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, int64(n)))
	require.NoError(tb, testApp.BankKeeper.SendCoinsFromAccountToModule(ctx, owner, fibretypes.ModuleName, total))
	testApp.FibreKeeper.SetEscrowAccount(ctx, fibretypes.EscrowAccount{
		Signer:           owner.String(),
		Balance:          total[0],
		AvailableBalance: sdk.NewInt64Coin(appconsts.BondDenom, 0),
	})
	// Distinct requested timestamps (1s apart) keep the withdrawal keys unique;
	// available = requested + delay stays in the past so all mature this block.
	base := time.Now().Add(-time.Duration(n) * time.Second).Add(-25 * time.Hour)
	delay := fibretypes.DefaultWithdrawalDelay
	for i := range n {
		requested := base.Add(time.Duration(i) * time.Second)
		testApp.FibreKeeper.SetWithdrawal(ctx, fibretypes.Withdrawal{
			Signer:             owner.String(),
			Amount:             sdk.NewInt64Coin(appconsts.BondDenom, 1),
			RequestedTimestamp: requested,
			AvailableTimestamp: requested.Add(delay),
		})
	}
}
