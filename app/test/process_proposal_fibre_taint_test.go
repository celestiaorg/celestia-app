package app_test

import (
	"bytes"
	"testing"
	"time"

	circuittypes "cosmossdk.io/x/circuit/types"
	"cosmossdk.io/x/feegrant"
	"github.com/celestiaorg/celestia-app/v11/app"
	"github.com/celestiaorg/celestia-app/v11/app/encoding"
	"github.com/celestiaorg/celestia-app/v11/fibre"
	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v11/pkg/user"
	testutil "github.com/celestiaorg/celestia-app/v11/test/util"
	"github.com/celestiaorg/celestia-app/v11/test/util/testfactory"
	fibretypes "github.com/celestiaorg/celestia-app/v11/x/fibre/types"
	"github.com/celestiaorg/go-square/v4/share"
	abci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/cosmos/cosmos-sdk/x/authz"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	icatypes "github.com/cosmos/ibc-go/v8/modules/apps/27-interchain-accounts/types"
	"github.com/stretchr/testify/require"
)

// fibreTaintFixture is a fresh chain with funded accounts and helpers to build
// proposals for them.
type fibreTaintFixture struct {
	app       *app.App
	accounts  []string
	addrs     []sdk.AccAddress
	signers   []*user.Signer
	blockTime time.Time
}

func newFibreTaintFixture(t *testing.T, n int) *fibreTaintFixture {
	t.Helper()
	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	accounts := testfactory.GenerateAccounts(n)
	testApp, kr := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams(), accounts...)
	infos := queryAccountInfo(testApp, accounts, kr)
	newSigner := newSignerFactory(t, kr, enc.TxConfig, accounts, infos)
	f := &fibreTaintFixture{app: testApp, accounts: accounts, blockTime: time.Now()}
	for i, account := range accounts {
		f.addrs = append(f.addrs, testfactory.GetAddress(kr, account))
		f.signers = append(f.signers, newSigner(i))
	}
	return f
}

func (f *fibreTaintFixture) ctx() sdk.Context {
	return f.app.NewUncachedContext(false, cmtproto.Header{
		ChainID: testutil.ChainID,
		Height:  f.app.LastBlockHeight(),
		Time:    f.blockTime,
	})
}

// tx signs msgs for account i and advances its sequence.
func (f *fibreTaintFixture) tx(t *testing.T, i int, msgs []sdk.Msg, opts ...user.TxOption) []byte {
	t.Helper()
	opts = append([]user.TxOption{user.SetGasLimit(1_000_000), user.SetFee(4_000)}, opts...)
	txBytes, _, err := f.signers[i].CreateTx(msgs, opts...)
	require.NoError(t, err)
	require.NoError(t, f.signers[i].IncrementSequence(f.accounts[i]))
	return txBytes
}

// pff signs a pay-for-fibre tx for account i and advances its sequence.
func (f *fibreTaintFixture) pff(t *testing.T, i int, opts ...user.TxOption) []byte {
	t.Helper()
	opts = append([]user.TxOption{user.SetGasLimit(1_000_000), user.SetFee(4_000)}, opts...)
	txBytes := newSignedPayForFibreTxAt(t, f.signers[i], f.accounts[i], true, f.blockTime, opts...)
	require.NoError(t, f.signers[i].IncrementSequence(f.accounts[i]))
	return txBytes
}

func (f *fibreTaintFixture) prepare(t *testing.T, txs [][]byte) [][]byte {
	t.Helper()
	resp, err := f.app.PrepareProposal(&abci.RequestPrepareProposal{
		Txs:    txs,
		Height: f.app.LastBlockHeight() + 1,
		Time:   f.blockTime,
	})
	require.NoError(t, err)
	return resp.Txs
}

func (f *fibreTaintFixture) process(t *testing.T, txs [][]byte) abci.ResponseProcessProposal_ProposalStatus {
	t.Helper()
	resp, err := f.app.ProcessProposal(processProposalRequestAt(t, f.app, txs, f.blockTime))
	require.NoError(t, err)
	return resp.Status
}

// requireNoUnpaidPayForFibre finalizes txs and checks that every pay-for-fibre
// among them settled.
func (f *fibreTaintFixture) requireNoUnpaidPayForFibre(t *testing.T, txs [][]byte) {
	t.Helper()
	require.Equal(t, abci.ResponseProcessProposal_ACCEPT, f.process(t, txs))
	resp, err := f.app.FinalizeBlock(&abci.RequestFinalizeBlock{
		Txs:    txs,
		Height: f.app.LastBlockHeight() + 1,
		Time:   f.blockTime,
		Hash:   f.app.LastCommitID().Hash,
	})
	require.NoError(t, err)
	require.Len(t, resp.TxResults, len(txs))
	dec := f.app.GetTxConfig().TxDecoder()
	for i, rawTx := range txs {
		sdkTx, err := dec(rawTx)
		if err != nil || fibretypes.PayForFibreMsg(sdkTx) == nil {
			continue
		}
		require.Equal(t, abci.CodeTypeOK, resp.TxResults[i].Code, "pay-for-fibre tx %d did not settle: %s", i, resp.TxResults[i].Log)
	}
}

// drainTo returns a MsgSend that leaves account i with only keep utia.
func (f *fibreTaintFixture) drainTo(t *testing.T, i, to int, keep int64) sdk.Msg {
	t.Helper()
	bal := f.app.BankKeeper.GetBalance(f.ctx(), f.addrs[i], appconsts.BondDenom)
	require.True(t, bal.Amount.Int64() > keep)
	amount := sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, bal.Amount.Int64()-keep))
	return banktypes.NewMsgSend(f.addrs[i], f.addrs[to], amount)
}

// TestPayForFibreAfterUnexecutedBankSpend covers a bank send that drains the
// escrow owner before a same-block deposit funds its pay-for-fibre. The proposal
// branch does not execute the send, so the deposit would look funded there.
func TestPayForFibreAfterUnexecutedBankSpend(t *testing.T) {
	f := newFibreTaintFixture(t, 2)
	payment := fibretypes.PaymentAmount(100)

	_, found := f.app.FibreKeeper.GetEscrowAccount(f.ctx(), f.addrs[0].String())
	require.False(t, found)

	send := f.tx(t, 0, []sdk.Msg{f.drainTo(t, 0, 1, payment.Amount.Int64()-1+3*4_000)})
	deposit := f.tx(t, 0, []sdk.Msg{&fibretypes.MsgDepositToEscrow{Signer: f.addrs[0].String(), Amount: payment}})
	pff := f.pff(t, 0)
	block := [][]byte{send, deposit, pff}

	kept := f.prepare(t, block)
	require.Contains(t, kept, send)
	require.Contains(t, kept, deposit)
	require.NotContains(t, kept, pff, "PrepareProposal must drop a pay-for-fibre that depends on an unexecuted send")

	require.Equal(t, abci.ResponseProcessProposal_REJECT, f.process(t, block))
	f.requireNoUnpaidPayForFibre(t, kept)
}

// TestPayForFibreAfterUnexecutedFeePayerSpend covers a fee payer with a funded
// escrow who drains its bank balance before its pay-for-fibre, so the
// pay-for-fibre's fee deduction fails in FinalizeBlock.
func TestPayForFibreAfterUnexecutedFeePayerSpend(t *testing.T) {
	f := newFibreTaintFixture(t, 3)
	payment := fibretypes.PaymentAmount(100).Amount.Int64()
	seedFibreEscrow(t, f.app, f.addrs[0], payment)
	seedFibreEscrow(t, f.app, f.addrs[2], payment)

	// Leaves exactly the send's own fee: the pay-for-fibre fee cannot be paid.
	send := f.tx(t, 0, []sdk.Msg{f.drainTo(t, 0, 1, 4_000)})
	pff := f.pff(t, 0)
	// accounts[2] pays for fibre in the same block without any other tx.
	untouched := f.pff(t, 2)

	kept := f.prepare(t, [][]byte{send, pff, untouched})
	require.Contains(t, kept, send)
	require.NotContains(t, kept, pff)
	require.Contains(t, kept, untouched, "a pay-for-fibre from an account no other tx acts for is kept")

	require.Equal(t, abci.ResponseProcessProposal_REJECT, f.process(t, [][]byte{send, pff}))
	f.requireNoUnpaidPayForFibre(t, kept)
}

// TestPayForFibreAfterUnexecutedFeegrantRevoke covers a fee granter revoking
// the allowance a later pay-for-fibre pays its fee with.
func TestPayForFibreAfterUnexecutedFeegrantRevoke(t *testing.T) {
	f := newFibreTaintFixture(t, 2)
	granter, grantee := 0, 1
	seedFibreEscrow(t, f.app, f.addrs[grantee], fibretypes.PaymentAmount(100).Amount.Int64())
	require.NoError(t, f.app.FeeGrantKeeper.GrantAllowance(f.ctx(), f.addrs[granter], f.addrs[grantee], &feegrant.BasicAllowance{}))

	revoke := f.tx(t, granter, []sdk.Msg{&feegrant.MsgRevokeAllowance{Granter: f.addrs[granter].String(), Grantee: f.addrs[grantee].String()}})
	pff := f.pff(t, grantee, user.SetFeeGranter(f.addrs[granter]))

	kept := f.prepare(t, [][]byte{revoke, pff})
	require.Contains(t, kept, revoke)
	require.NotContains(t, kept, pff)
	require.Equal(t, abci.ResponseProcessProposal_REJECT, f.process(t, [][]byte{revoke, pff}))
	f.requireNoUnpaidPayForFibre(t, kept)
}

// TestPayForFibreAfterUnexecutedAuthzSpend covers a grantee draining the escrow
// owner's bank balance through authz MsgExec, where the owner is not a signer
// of the tx.
func TestPayForFibreAfterUnexecutedAuthzSpend(t *testing.T) {
	f := newFibreTaintFixture(t, 2)
	owner, grantee := 0, 1
	seedFibreEscrow(t, f.app, f.addrs[owner], fibretypes.PaymentAmount(100).Amount.Int64())
	expiration := f.blockTime.Add(time.Hour)
	require.NoError(t, f.app.AuthzKeeper.SaveGrant(f.ctx(), f.addrs[grantee], f.addrs[owner],
		authz.NewGenericAuthorization(sdk.MsgTypeURL(&banktypes.MsgSend{})), &expiration))

	exec := authz.NewMsgExec(f.addrs[grantee], []sdk.Msg{f.drainTo(t, owner, grantee, 0)})
	execTx := f.tx(t, grantee, []sdk.Msg{&exec})
	pff := f.pff(t, owner)

	kept := f.prepare(t, [][]byte{execTx, pff})
	require.Contains(t, kept, execTx)
	require.NotContains(t, kept, pff)
	require.Equal(t, abci.ResponseProcessProposal_REJECT, f.process(t, [][]byte{execTx, pff}))
	f.requireNoUnpaidPayForFibre(t, kept)
}

// TestPayForFibreWithInterchainAccountFeeGranter covers a fee granter that is
// an interchain account, which an IBC packet can drain or make revoke its
// allowance without the proposal branch seeing it.
func TestPayForFibreWithInterchainAccountFeeGranter(t *testing.T) {
	f := newFibreTaintFixture(t, 1)
	seedFibreEscrow(t, f.app, f.addrs[0], fibretypes.PaymentAmount(100).Amount.Int64())

	ctx := f.ctx()
	icaAddr := sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address())
	base := authtypes.NewBaseAccountWithAddress(icaAddr)
	base.AccountNumber = f.app.AccountKeeper.NextAccountNumber(ctx)
	f.app.AccountKeeper.SetAccount(ctx, icatypes.NewInterchainAccount(base, "owner"))
	require.NoError(t, f.app.BankKeeper.SendCoins(ctx, f.addrs[0], icaAddr, sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, 1_000_000))))
	require.NoError(t, f.app.FeeGrantKeeper.GrantAllowance(ctx, icaAddr, f.addrs[0], &feegrant.BasicAllowance{}))

	pff := f.pff(t, 0, user.SetFeeGranter(icaAddr))
	require.NotContains(t, f.prepare(t, [][]byte{pff}), pff)
	require.Equal(t, abci.ResponseProcessProposal_REJECT, f.process(t, [][]byte{pff}))
}

// TestPayForFibreAfterOwnTimeoutIsKept checks that a payment promise timeout,
// which the proposal branch executes, does not stop the same account's
// pay-for-fibre from settling in the block.
func TestPayForFibreAfterOwnTimeoutIsKept(t *testing.T) {
	f := newFibreTaintFixture(t, 1)
	seedFibreEscrow(t, f.app, f.addrs[0], 2*fibretypes.PaymentAmount(100).Amount.Int64())

	timeout := newSignedPaymentPromiseTimeoutTx(t, f.signers[0], f.accounts[0], f.blockTime.Add(-2*time.Hour))
	require.NoError(t, f.signers[0].IncrementSequence(f.accounts[0]))
	pff := f.pff(t, 0)

	kept := f.prepare(t, [][]byte{timeout, pff})
	require.Equal(t, [][]byte{timeout, pff}, kept)
	f.requireNoUnpaidPayForFibre(t, kept)
}

// TestPayForFibreAfterBlobTxWithTouchedGranter covers a blob tx whose fee
// granter an earlier tx acts for: its ante handler may fail in FinalizeBlock,
// leaving its signer's sequence behind the proposal branch, so that signer's
// later pay-for-fibre must not settle on the branch either.
func TestPayForFibreAfterBlobTxWithTouchedGranter(t *testing.T) {
	f := newFibreTaintFixture(t, 3)
	granter, payer := 0, 1
	seedFibreEscrow(t, f.app, f.addrs[payer], fibretypes.PaymentAmount(100).Amount.Int64())
	require.NoError(t, f.app.FeeGrantKeeper.GrantAllowance(f.ctx(), f.addrs[granter], f.addrs[payer], &feegrant.BasicAllowance{}))

	revoke := f.tx(t, granter, []sdk.Msg{&feegrant.MsgRevokeAllowance{Granter: f.addrs[granter].String(), Grantee: f.addrs[payer].String()}})
	blob, err := share.NewV0Blob(share.RandomBlobNamespace(), bytes.Repeat([]byte{1}, 100))
	require.NoError(t, err)
	blobTx, _, err := f.signers[payer].CreatePayForBlobs(f.accounts[payer], []*share.Blob{blob},
		user.SetGasLimit(1_000_000), user.SetFee(4_000), user.SetFeeGranter(f.addrs[granter]))
	require.NoError(t, err)
	require.NoError(t, f.signers[payer].IncrementSequence(f.accounts[payer]))
	pff := f.pff(t, payer)

	kept := f.prepare(t, [][]byte{revoke, blobTx, pff})
	require.Contains(t, kept, revoke)
	require.Contains(t, kept, blobTx)
	require.NotContains(t, kept, pff)
	require.Equal(t, abci.ResponseProcessProposal_REJECT, f.process(t, [][]byte{revoke, blobTx, pff}))
}

// timeoutMsg builds a MsgPaymentPromiseTimeout submitted by submitter for a
// promise signed by account owner.
func (f *fibreTaintFixture) timeoutMsg(t *testing.T, owner int, submitter sdk.AccAddress, creation time.Time) *fibretypes.MsgPaymentPromiseTimeout {
	t.Helper()
	ownerPub := f.signers[owner].Account(f.accounts[owner]).PubKey().(*secp256k1.PubKey)
	promise := fibretypes.PaymentPromise{
		ChainId:           testutil.ChainID,
		Height:            1,
		Namespace:         share.MustNewV0Namespace(bytes.Repeat([]byte{0x2}, share.NamespaceVersionZeroIDSize)).Bytes(),
		BlobSize:          100,
		Commitment:        make([]byte, 32),
		CreationTimestamp: creation.Truncate(time.Second),
		SignerPublicKey:   *ownerPub,
	}
	pp := fibre.PaymentPromise{}
	require.NoError(t, pp.FromProto(&promise))
	signBytes, err := pp.SignBytes()
	require.NoError(t, err)
	promise.Signature, _, err = f.signers[owner].Keyring().Sign(f.accounts[owner], signBytes, signing.SignMode_SIGN_MODE_DIRECT)
	require.NoError(t, err)
	return &fibretypes.MsgPaymentPromiseTimeout{Signer: submitter.String(), PaymentPromise: promise}
}

// requireVictimPayForFibreKept checks that the victim's pay-for-fibre survives
// the attacker's tx in PrepareProposal and ProcessProposal and settles.
func (f *fibreTaintFixture) requireVictimPayForFibreKept(t *testing.T, attackTx, victimPFF []byte) {
	t.Helper()
	kept := f.prepare(t, [][]byte{attackTx, victimPFF})
	require.Contains(t, kept, attackTx)
	require.Contains(t, kept, victimPFF, "the victim's pay-for-fibre must not be censored")
	f.requireNoUnpaidPayForFibre(t, kept)
}

// The tests below check that a tx cannot drop an unrelated account's
// pay-for-fibre by naming it in messages that cannot act for it.

func TestPayForFibreNotCensoredByUnprivilegedCircuitBreakerTrip(t *testing.T) {
	f := newFibreTaintFixture(t, 2)
	attacker, victim := 0, 1
	seedFibreEscrow(t, f.app, f.addrs[victim], fibretypes.PaymentAmount(100).Amount.Int64())

	trip := f.tx(t, attacker, []sdk.Msg{&circuittypes.MsgTripCircuitBreaker{
		Authority:   f.addrs[attacker].String(),
		MsgTypeUrls: []string{sdk.MsgTypeURL(&fibretypes.MsgPayForFibre{})},
	}})
	f.requireVictimPayForFibreKept(t, trip, f.pff(t, victim))
}

func TestPayForFibreDroppedAfterCircuitBreakerAdminTrip(t *testing.T) {
	f := newFibreTaintFixture(t, 2)
	admin, other := 0, 1
	seedFibreEscrow(t, f.app, f.addrs[other], fibretypes.PaymentAmount(100).Amount.Int64())
	require.NoError(t, f.app.CircuitKeeper.Permissions.Set(f.ctx(), f.addrs[admin],
		circuittypes.Permissions{Level: circuittypes.Permissions_LEVEL_SUPER_ADMIN}))

	trip := f.tx(t, admin, []sdk.Msg{&circuittypes.MsgTripCircuitBreaker{
		Authority:   f.addrs[admin].String(),
		MsgTypeUrls: []string{sdk.MsgTypeURL(&fibretypes.MsgPayForFibre{})},
	}})
	pff := f.pff(t, other)
	kept := f.prepare(t, [][]byte{trip, pff})
	require.Contains(t, kept, trip)
	require.NotContains(t, kept, pff)
	require.Equal(t, abci.ResponseProcessProposal_REJECT, f.process(t, [][]byte{trip, pff}))
	f.requireNoUnpaidPayForFibre(t, kept)
}

func TestPayForFibreNotCensoredByUnresolvableAuthzExec(t *testing.T) {
	f := newFibreTaintFixture(t, 2)
	attacker, victim := 0, 1
	seedFibreEscrow(t, f.app, f.addrs[victim], fibretypes.PaymentAmount(100).Amount.Int64())

	bogus := &banktypes.MsgSend{FromAddress: "not-bech32", ToAddress: f.addrs[attacker].String(), Amount: sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, 1))}
	exec := authz.NewMsgExec(f.addrs[attacker], []sdk.Msg{bogus})
	f.requireVictimPayForFibreKept(t, f.tx(t, attacker, []sdk.Msg{&exec}), f.pff(t, victim))
}

func TestPayForFibreNotCensoredByAuthzExecWithoutGrant(t *testing.T) {
	f := newFibreTaintFixture(t, 2)
	attacker, victim := 0, 1
	seedFibreEscrow(t, f.app, f.addrs[victim], fibretypes.PaymentAmount(100).Amount.Int64())

	exec := authz.NewMsgExec(f.addrs[attacker], []sdk.Msg{f.drainTo(t, victim, attacker, 0)})
	f.requireVictimPayForFibreKept(t, f.tx(t, attacker, []sdk.Msg{&exec}), f.pff(t, victim))
}

func TestPayForFibreNotCensoredByCopiedInvalidTimeout(t *testing.T) {
	f := newFibreTaintFixture(t, 2)
	attacker, victim := 0, 1
	seedFibreEscrow(t, f.app, f.addrs[victim], fibretypes.PaymentAmount(100).Amount.Int64())

	// The victim's promise has not timed out, so the copied timeout cannot
	// settle. The send keeps the tx from being executed on the branch.
	send := banktypes.NewMsgSend(f.addrs[attacker], f.addrs[attacker], sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, 1)))
	timeout := f.timeoutMsg(t, victim, f.addrs[attacker], f.blockTime)
	f.requireVictimPayForFibreKept(t, f.tx(t, attacker, []sdk.Msg{send, timeout}), f.pff(t, victim))
}

func TestPayForFibreDroppedAfterValidTimeoutInMixedTx(t *testing.T) {
	f := newFibreTaintFixture(t, 2)
	submitter, owner := 0, 1
	seedFibreEscrow(t, f.app, f.addrs[owner], fibretypes.PaymentAmount(100).Amount.Int64())

	// An expired promise: the timeout debits the owner's whole escrow in
	// FinalizeBlock, which the branch does not execute for a mixed tx.
	send := banktypes.NewMsgSend(f.addrs[submitter], f.addrs[submitter], sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, 1)))
	timeout := f.timeoutMsg(t, owner, f.addrs[submitter], f.blockTime.Add(-2*time.Hour))
	mixed := f.tx(t, submitter, []sdk.Msg{send, timeout})
	pff := f.pff(t, owner)

	kept := f.prepare(t, [][]byte{mixed, pff})
	require.Contains(t, kept, mixed)
	require.NotContains(t, kept, pff)
	require.Equal(t, abci.ResponseProcessProposal_REJECT, f.process(t, [][]byte{mixed, pff}))
	f.requireNoUnpaidPayForFibre(t, kept)
}

// TestTxFailingMessageValidationBeforePayForFibre covers a tx that passes the
// ante handler but fails message ValidateBasic, which FinalizeBlock checks
// before the ante handler: its signer's sequence advances on the proposal
// branch but not in FinalizeBlock, so the signer's next pay-for-fibre would
// fail there.
func TestTxFailingMessageValidationBeforePayForFibre(t *testing.T) {
	f := newFibreTaintFixture(t, 1)
	seedFibreEscrow(t, f.app, f.addrs[0], 2*fibretypes.PaymentAmount(100).Amount.Int64())

	timeout := f.timeoutMsg(t, 0, f.addrs[0], f.blockTime.Add(-2*time.Hour))
	timeout.PaymentPromise.ChainId = "" // fails PaymentPromise.ValidateBasic
	require.Error(t, timeout.ValidateBasic())
	invalid := f.tx(t, 0, []sdk.Msg{timeout})
	pff := f.pff(t, 0)

	kept := f.prepare(t, [][]byte{invalid, pff})
	require.NotContains(t, kept, invalid)
	require.Equal(t, abci.ResponseProcessProposal_REJECT, f.process(t, [][]byte{invalid, pff}))
	f.requireNoUnpaidPayForFibre(t, kept)
}
