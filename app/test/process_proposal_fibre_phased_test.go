package app_test

import (
	"testing"
	"time"

	"cosmossdk.io/math"
	"cosmossdk.io/x/feegrant"
	"github.com/celestiaorg/celestia-app/v11/app"
	"github.com/celestiaorg/celestia-app/v11/app/encoding"
	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v11/pkg/user"
	testutil "github.com/celestiaorg/celestia-app/v11/test/util"
	"github.com/celestiaorg/celestia-app/v11/test/util/testfactory"
	fibretypes "github.com/celestiaorg/celestia-app/v11/x/fibre/types"
	abci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/authz"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"
)

// phasedFixture is a committed test app with funded accounts for the
// pay-for-fibre FinalizeBlock tests below.
type phasedFixture struct {
	testApp  *app.App
	accounts []string
	addrs    []sdk.AccAddress
	signers  []*user.Signer
}

func newPhasedFixture(t *testing.T, n int) phasedFixture {
	t.Helper()
	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	accounts := testfactory.GenerateAccounts(n)
	testApp, kr := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams(), accounts...)
	commitBlock(t, testApp)
	infos := queryAccountInfo(testApp, accounts, kr)
	newSigner := newSignerFactory(t, kr, enc.TxConfig, accounts, infos)
	f := phasedFixture{testApp: testApp, accounts: accounts}
	for i, account := range accounts {
		f.addrs = append(f.addrs, testfactory.GetAddress(kr, account))
		f.signers = append(f.signers, newSigner(i))
	}
	return f
}

func (f phasedFixture) ctx() sdk.Context {
	return f.testApp.NewUncachedContext(false, cmtproto.Header{ChainID: testutil.ChainID, Height: f.testApp.LastBlockHeight(), Time: time.Now()})
}

// tx creates a tx from account i and bumps its sequence.
func (f phasedFixture) tx(t *testing.T, i int, msgs ...sdk.Msg) []byte {
	t.Helper()
	raw, _, err := f.signers[i].CreateTx(msgs, user.SetGasLimit(200_000), user.SetFee(4_000))
	require.NoError(t, err)
	require.NoError(t, f.signers[i].IncrementSequence(f.accounts[i]))
	return raw
}

// run checks the block through PrepareProposal, ProcessProposal and
// FinalizeBlock and returns the FinalizeBlock tx results.
func (f phasedFixture) run(t *testing.T, txs [][]byte, blockTime time.Time) (prepared [][]byte, status abci.ResponseProcessProposal_ProposalStatus, results []*abci.ExecTxResult) {
	t.Helper()
	prep, err := f.testApp.PrepareProposal(&abci.RequestPrepareProposal{Txs: txs, Height: f.testApp.LastBlockHeight() + 1, Time: blockTime})
	require.NoError(t, err)
	resp, err := f.testApp.ProcessProposal(processProposalRequestAt(t, f.testApp, txs, blockTime))
	require.NoError(t, err)
	fin, err := f.testApp.FinalizeBlock(&abci.RequestFinalizeBlock{Txs: txs, Height: f.testApp.LastBlockHeight() + 1, Time: blockTime, Hash: f.testApp.LastCommitID().Hash})
	require.NoError(t, err)
	return prep.Txs, resp.Status, fin.TxResults
}

// TestPayForFibreSettlesAfterEarlierFeeSpend checks that an earlier tx in the
// block that spends a pay-for-fibre's fee funds cannot make it fail in
// FinalizeBlock. FinalizeBlock runs every tx's ante handler before any tx's
// messages, so the pay-for-fibre's fee is charged before the earlier tx's
// messages run, matching what proposal checks.
func TestPayForFibreSettlesAfterEarlierFeeSpend(t *testing.T) {
	const fee = 4_000
	payment := fibretypes.PaymentAmount(100).Amount.Int64()
	tests := []struct {
		name string
		// build returns the block's txs; the pay-for-fibre is last.
		build func(t *testing.T, f phasedFixture) [][]byte
	}{
		{
			name: "fee payer sends its balance away",
			build: func(t *testing.T, f phasedFixture) [][]byte {
				seedFibreEscrow(t, f.testApp, f.addrs[0], payment)
				balance := f.testApp.BankKeeper.GetBalance(f.ctx(), f.addrs[0], appconsts.BondDenom)
				drain := f.tx(t, 0, banktypes.NewMsgSend(f.addrs[0], f.addrs[1], sdk.NewCoins(balance.SubAmount(math.NewInt(2*fee)))))
				return [][]byte{drain, newSignedPayForFibreTx(t, f.signers[0], f.accounts[0], true)}
			},
		},
		{
			name: "fee granter revokes the allowance",
			build: func(t *testing.T, f phasedFixture) [][]byte {
				require.NoError(t, f.testApp.FeeGrantKeeper.GrantAllowance(f.ctx(), f.addrs[0], f.addrs[1], &feegrant.BasicAllowance{}))
				seedFibreEscrow(t, f.testApp, f.addrs[1], payment)
				revoke := f.tx(t, 0, &feegrant.MsgRevokeAllowance{Granter: f.addrs[0].String(), Grantee: f.addrs[1].String()})
				pff := newSignedPayForFibreTxWithOpts(t, f.signers[1], f.accounts[1], true, user.SetGasLimit(1_000_000), user.SetFee(fee), user.SetFeeGranter(f.addrs[0]))
				return [][]byte{revoke, pff}
			},
		},
		{
			name: "authz grantee sends the fee payer's balance away",
			build: func(t *testing.T, f phasedFixture) [][]byte {
				exp := time.Now().Add(time.Hour)
				require.NoError(t, f.testApp.AuthzKeeper.SaveGrant(f.ctx(), f.addrs[1], f.addrs[0], authz.NewGenericAuthorization(sdk.MsgTypeURL(&banktypes.MsgSend{})), &exp))
				seedFibreEscrow(t, f.testApp, f.addrs[0], payment)
				balance := f.testApp.BankKeeper.GetBalance(f.ctx(), f.addrs[0], appconsts.BondDenom)
				exec := authz.NewMsgExec(f.addrs[1], []sdk.Msg{banktypes.NewMsgSend(f.addrs[0], f.addrs[1], sdk.NewCoins(balance.SubAmount(math.NewInt(fee))))})
				return [][]byte{f.tx(t, 1, &exec), newSignedPayForFibreTx(t, f.signers[0], f.accounts[0], true)}
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newPhasedFixture(t, 2)
			txs := tc.build(t, f)
			prepared, status, results := f.run(t, txs, time.Now())
			require.Equal(t, txs, prepared)
			require.Equal(t, abci.ResponseProcessProposal_ACCEPT, status)
			for i, r := range results {
				require.Equal(t, abci.CodeTypeOK, r.Code, "tx %d: %s", i, r.Log)
			}
		})
	}
}

// TestPayForFibreSettlementGasIndependentOfEarlierTxs checks that a
// pay-for-fibre with the least gas ProcessProposal accepts still settles in
// FinalizeBlock after an unrelated deposit that proposal does not execute.
// The deposit grows the fibre module balance that settlement writes, so
// metered settlement gas would differ between the two.
func TestPayForFibreSettlementGasIndependentOfEarlierTxs(t *testing.T) {
	f := newPhasedFixture(t, 2)
	payment := fibretypes.PaymentAmount(100).Amount.Int64()
	seedFibreEscrow(t, f.testApp, f.addrs[0], 10*payment)
	blockTime := time.Now()

	balance := f.testApp.BankKeeper.GetBalance(f.ctx(), f.addrs[1], appconsts.BondDenom)
	deposit := f.tx(t, 1, &fibretypes.MsgDepositToEscrow{Signer: f.addrs[1].String(), Amount: balance.SubAmount(math.NewInt(100_000))})
	// A fixed creation time keeps the tx bytes, and so its ante gas, stable
	// across the gas limits tried below.
	pffWithGas := func(gas uint64) []byte {
		return newSignedPayForFibreTxAt(t, f.signers[0], f.accounts[0], true, blockTime.Add(-time.Second), user.SetGasLimit(gas), user.SetFee(4_000))
	}

	accepts := func(gas uint64) bool {
		resp, err := f.testApp.ProcessProposal(processProposalRequestAt(t, f.testApp, [][]byte{deposit, pffWithGas(gas)}, blockTime))
		require.NoError(t, err)
		return resp.Status == abci.ResponseProcessProposal_ACCEPT
	}
	lo, hi := uint64(10_000), uint64(1_000_000)
	require.True(t, accepts(hi))
	for lo < hi {
		if mid := (lo + hi) / 2; accepts(mid) {
			hi = mid
		} else {
			lo = mid + 1
		}
	}

	txs := [][]byte{deposit, pffWithGas(lo)}
	prepared, status, results := f.run(t, txs, blockTime)
	require.Equal(t, txs, prepared)
	require.Equal(t, abci.ResponseProcessProposal_ACCEPT, status)
	require.Equal(t, abci.CodeTypeOK, results[0].Code, results[0].Log)
	require.Equal(t, abci.CodeTypeOK, results[1].Code, "the pay-for-fibre must settle with the gas proposal accepted: %s", results[1].Log)
}
