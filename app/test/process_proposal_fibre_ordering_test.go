package app_test

import (
	"bytes"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v10/app"
	"github.com/celestiaorg/celestia-app/v10/app/encoding"
	"github.com/celestiaorg/celestia-app/v10/fibre"
	"github.com/celestiaorg/celestia-app/v10/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v10/pkg/user"
	testutil "github.com/celestiaorg/celestia-app/v10/test/util"
	"github.com/celestiaorg/celestia-app/v10/test/util/testfactory"
	fibretypes "github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	"github.com/celestiaorg/go-square/v4/share"
	abci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	"github.com/stretchr/testify/require"
)

// TestProcessProposalTimeoutThenPayForFibre guards against an underpaid
// pay-for-fibre: a timeout ordered before it debits the same escrow in
// FinalizeBlock. ProcessProposal replays that debit, so it rejects the block
// unless the escrow still covers the pay-for-fibre.
func TestProcessProposalTimeoutThenPayForFibre(t *testing.T) {
	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	accounts := testfactory.GenerateAccounts(3)
	testApp, kr := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams(), accounts...)
	infos := queryAccountInfo(testApp, accounts, kr)
	newSigner := newSignerFactory(t, kr, enc.TxConfig, accounts, infos)

	// Every factory promise has BlobSize=100 and costs one payment.
	payment := fibretypes.PaymentAmount(100).Amount.Int64()

	// accounts[0]: exactly one payment -> the timeout drains it, so the
	// pay-for-fibre can no longer settle and the block must be rejected.
	// accounts[1]: two payments -> the timeout and the pay-for-fibre both settle.
	// accounts[2]: one payment, pay-for-fibre alone -> accepted (control).
	seedFibreEscrow(t, testApp, testfactory.GetAddress(kr, accounts[0]), payment)
	seedFibreEscrow(t, testApp, testfactory.GetAddress(kr, accounts[1]), 2*payment)
	seedFibreEscrow(t, testApp, testfactory.GetAddress(kr, accounts[2]), payment)

	// Default params: WithdrawalDelay=24h, PaymentPromiseTimeout=1h. A creation
	// timestamp 2h in the past is both fresh (after now-24h) and expired (before
	// now-1h), so the timeout path accepts it.
	timeoutCreation := time.Now().Add(-2 * time.Hour)

	sig0 := newSigner(0)
	timeout0 := newSignedPaymentPromiseTimeoutTx(t, sig0, accounts[0], timeoutCreation)
	require.NoError(t, sig0.IncrementSequence(accounts[0]))
	pff0 := newSignedPayForFibreTxAt(t, sig0, accounts[0], true, time.Now(), user.SetGasLimit(1_000_000), user.SetFee(4_000))

	sig1 := newSigner(1)
	timeout1 := newSignedPaymentPromiseTimeoutTx(t, sig1, accounts[1], timeoutCreation)
	require.NoError(t, sig1.IncrementSequence(accounts[1]))
	pff1 := newSignedPayForFibreTxAt(t, sig1, accounts[1], true, time.Now(), user.SetGasLimit(1_000_000), user.SetFee(4_000))

	pff2 := newSignedPayForFibreTx(t, newSigner(2), accounts[2], true)

	tests := []struct {
		name           string
		txs            [][]byte
		expectedStatus abci.ResponseProcessProposal_ProposalStatus
	}{
		{
			name:           "reject: timeout drains escrow below the pay-for-fibre charge",
			txs:            [][]byte{timeout0, pff0},
			expectedStatus: abci.ResponseProcessProposal_REJECT,
		},
		{
			name:           "accept: escrow covers both the timeout and the pay-for-fibre",
			txs:            [][]byte{timeout1, pff1},
			expectedStatus: abci.ResponseProcessProposal_ACCEPT,
		},
		{
			name:           "accept: pay-for-fibre alone fits the escrow",
			txs:            [][]byte{pff2},
			expectedStatus: abci.ResponseProcessProposal_ACCEPT,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := testApp.ProcessProposal(processProposalRequest(t, testApp, tc.txs))
			require.NoError(t, err)
			require.Equal(t, tc.expectedStatus, resp.Status)
		})
	}
}

// newSignedPaymentPromiseTimeoutTx builds a signed MsgPaymentPromiseTimeout tx
// for an expired, owner-signed promise on the account's escrow.
func newSignedPaymentPromiseTimeoutTx(t *testing.T, signer *user.Signer, account string, creation time.Time) []byte {
	t.Helper()
	ownerPub := signer.Account(account).PubKey().(*secp256k1.PubKey)
	promise := fibretypes.PaymentPromise{
		ChainId:           testutil.ChainID,
		Height:            1,
		Namespace:         share.MustNewV0Namespace(bytes.Repeat([]byte{0x2}, share.NamespaceVersionZeroIDSize)).Bytes(),
		BlobSize:          100,
		BlobVersion:       0,
		Commitment:        make([]byte, 32),
		CreationTimestamp: creation.Truncate(time.Second),
		SignerPublicKey:   *ownerPub,
		Signature:         make([]byte, 64),
	}

	pp := fibre.PaymentPromise{}
	require.NoError(t, pp.FromProto(&promise))
	signBytes, err := pp.SignBytes()
	require.NoError(t, err)
	promise.Signature, _, err = signer.Keyring().Sign(account, signBytes, signing.SignMode_SIGN_MODE_DIRECT)
	require.NoError(t, err)

	msg := &fibretypes.MsgPaymentPromiseTimeout{
		Signer:         sdk.AccAddress(ownerPub.Address()).String(),
		PaymentPromise: promise,
	}
	txBytes, _, err := signer.CreateTx([]sdk.Msg{msg}, user.SetGasLimit(1_000_000), user.SetFee(4_000))
	require.NoError(t, err)
	return txBytes
}

// TestPrepareProposalTimeoutThenPayForFibre checks that the proposer drops a
// pay-for-fibre a preceding timeout makes unpayable, so its system blob never
// reaches the square.
func TestPrepareProposalTimeoutThenPayForFibre(t *testing.T) {
	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	accounts := testfactory.GenerateAccounts(2)
	testApp, kr := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams(), accounts...)
	infos := queryAccountInfo(testApp, accounts, kr)
	newSigner := newSignerFactory(t, kr, enc.TxConfig, accounts, infos)

	payment := fibretypes.PaymentAmount(100).Amount.Int64()
	seedFibreEscrow(t, testApp, testfactory.GetAddress(kr, accounts[0]), payment)   // one payment
	seedFibreEscrow(t, testApp, testfactory.GetAddress(kr, accounts[1]), 2*payment) // two payments

	timeoutCreation := time.Now().Add(-2 * time.Hour)

	sig0 := newSigner(0)
	timeout0 := newSignedPaymentPromiseTimeoutTx(t, sig0, accounts[0], timeoutCreation)
	require.NoError(t, sig0.IncrementSequence(accounts[0]))
	pff0 := newSignedPayForFibreTxAt(t, sig0, accounts[0], true, time.Now(), user.SetGasLimit(1_000_000), user.SetFee(4_000))

	sig1 := newSigner(1)
	timeout1 := newSignedPaymentPromiseTimeoutTx(t, sig1, accounts[1], timeoutCreation)
	require.NoError(t, sig1.IncrementSequence(accounts[1]))
	pff1 := newSignedPayForFibreTxAt(t, sig1, accounts[1], true, time.Now(), user.SetGasLimit(1_000_000), user.SetFee(4_000))

	t.Run("drops the pay-for-fibre the timeout makes unpayable", func(t *testing.T) {
		resp, err := testApp.PrepareProposal(&abci.RequestPrepareProposal{
			Txs:    [][]byte{timeout0, pff0},
			Height: testApp.LastBlockHeight() + 1,
			Time:   time.Now(),
		})
		require.NoError(t, err)
		require.Contains(t, resp.Txs, timeout0, "the timeout tx should be kept")
		require.NotContains(t, resp.Txs, pff0, "the unpayable pay-for-fibre must be dropped")
	})

	t.Run("keeps both when the escrow covers them", func(t *testing.T) {
		resp, err := testApp.PrepareProposal(&abci.RequestPrepareProposal{
			Txs:    [][]byte{timeout1, pff1},
			Height: testApp.LastBlockHeight() + 1,
			Time:   time.Now(),
		})
		require.NoError(t, err)
		require.Contains(t, resp.Txs, timeout1)
		require.Contains(t, resp.Txs, pff1)
	})
}

// TestProcessProposalMaturingWithdrawalThenPayForFibre guards PROTOCO-2429: a
// withdrawal paid out by the BeginBlocker debits escrow before any tx, so a
// pay-for-fibre it leaves underfunded must be rejected.
func TestProcessProposalMaturingWithdrawalThenPayForFibre(t *testing.T) {
	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	accounts := testfactory.GenerateAccounts(2)
	testApp, kr := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams(), accounts...)
	infos := queryAccountInfo(testApp, accounts, kr)
	newSigner := newSignerFactory(t, kr, enc.TxConfig, accounts, infos)

	payment := fibretypes.PaymentAmount(100).Amount.Int64()
	// accounts[0]: one payment, all locked in a matured withdrawal -> the payout
	// drains the escrow and the pay-for-fibre can no longer settle.
	// accounts[1]: two payments, one locked -> one payment survives the payout so
	// the pay-for-fibre still settles.
	seedMaturedWithdrawal(t, testApp, testfactory.GetAddress(kr, accounts[0]), payment, payment)
	seedMaturedWithdrawal(t, testApp, testfactory.GetAddress(kr, accounts[1]), 2*payment, payment)

	pff0 := newSignedPayForFibreTx(t, newSigner(0), accounts[0], true)
	pff1 := newSignedPayForFibreTx(t, newSigner(1), accounts[1], true)

	tests := []struct {
		name           string
		txs            [][]byte
		expectedStatus abci.ResponseProcessProposal_ProposalStatus
	}{
		{
			name:           "reject: matured withdrawal drains escrow below the charge",
			txs:            [][]byte{pff0},
			expectedStatus: abci.ResponseProcessProposal_REJECT,
		},
		{
			name:           "accept: escrow still covers the charge after the payout",
			txs:            [][]byte{pff1},
			expectedStatus: abci.ResponseProcessProposal_ACCEPT,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := testApp.ProcessProposal(processProposalRequest(t, testApp, tc.txs))
			require.NoError(t, err)
			require.Equal(t, tc.expectedStatus, resp.Status)
		})
	}
}

// seedMaturedWithdrawal seeds an escrow holding balance coins with amount of
// them locked in a withdrawal that is already available, so the next
// BeginBlocker pays it out and lowers the balance by amount.
func seedMaturedWithdrawal(t *testing.T, testApp *app.App, owner sdk.AccAddress, balance, amount int64) {
	t.Helper()
	ctx := testApp.NewUncachedContext(false, cmtproto.Header{
		ChainID: testutil.ChainID,
		Height:  testApp.LastBlockHeight(),
		Time:    time.Now(),
	})
	coins := sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, balance))
	require.NoError(t, testApp.BankKeeper.SendCoinsFromAccountToModule(ctx, owner, fibretypes.ModuleName, coins))
	testApp.FibreKeeper.SetEscrowAccount(ctx, fibretypes.EscrowAccount{
		Signer:           owner.String(),
		Balance:          sdk.NewInt64Coin(appconsts.BondDenom, balance),
		AvailableBalance: sdk.NewInt64Coin(appconsts.BondDenom, balance-amount),
	})
	testApp.FibreKeeper.SetWithdrawal(ctx, fibretypes.Withdrawal{
		Signer:             owner.String(),
		Amount:             sdk.NewInt64Coin(appconsts.BondDenom, amount),
		RequestedTimestamp: time.Now().Add(-48 * time.Hour),
		AvailableTimestamp: time.Now().Add(-24 * time.Hour),
	})
}
