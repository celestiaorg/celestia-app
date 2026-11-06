package app_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"slices"
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
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	icatypes "github.com/cosmos/ibc-go/v8/modules/apps/27-interchain-accounts/types"
	ibctransfertypes "github.com/cosmos/ibc-go/v8/modules/apps/transfer/types"
	clienttypes "github.com/cosmos/ibc-go/v8/modules/core/02-client/types"
	channeltypes "github.com/cosmos/ibc-go/v8/modules/core/04-channel/types"
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

// requireOtherPayForFibreDropped checks that a pay-for-fibre from an account the
// attack txs never name is dropped by PrepareProposal and makes
// ProcessProposal reject the block.
func (f *fibreTaintFixture) requireOtherPayForFibreDropped(t *testing.T, attack [][]byte, pff []byte) {
	t.Helper()
	block := append(append([][]byte{}, attack...), pff)
	kept := f.prepare(t, block)
	for _, tx := range attack {
		require.Contains(t, kept, tx)
	}
	require.NotContains(t, kept, pff)
	require.Equal(t, abci.ResponseProcessProposal_REJECT, f.process(t, block))
	f.requireNoUnpaidPayForFibre(t, kept)
}

// TestPayForFibreDroppedAfterUnmodelableMessage covers messages the proposal
// branch cannot reason about without executing them. Each could change what
// a later pay-for-fibre reads in FinalizeBlock (the circuit breaker, fibre
// params, or anyone's escrow through authz), so no pay-for-fibre settles
// after one in the same block, whoever signed it.
func TestPayForFibreDroppedAfterUnmodelableMessage(t *testing.T) {
	const actor, other, admin = 0, 1, 2
	trip := func(authority sdk.AccAddress) *circuittypes.MsgTripCircuitBreaker {
		return &circuittypes.MsgTripCircuitBreaker{
			Authority:   authority.String(),
			MsgTypeUrls: []string{sdk.MsgTypeURL(&fibretypes.MsgPayForFibre{})},
		}
	}
	tests := []struct {
		name   string
		attack func(t *testing.T, f *fibreTaintFixture) [][]byte
	}{
		{"unprivileged circuit trip", func(t *testing.T, f *fibreTaintFixture) [][]byte {
			return [][]byte{f.tx(t, actor, []sdk.Msg{trip(f.addrs[actor])})}
		}},
		{"circuit admin trip", func(t *testing.T, f *fibreTaintFixture) [][]byte {
			return [][]byte{f.tx(t, admin, []sdk.Msg{trip(f.addrs[admin])})}
		}},
		{"exec of a circuit trip naming the gov authority", func(t *testing.T, f *fibreTaintFixture) [][]byte {
			exec := authz.NewMsgExec(f.addrs[actor], []sdk.Msg{trip(authtypes.NewModuleAddress(govtypes.ModuleName))})
			return [][]byte{f.tx(t, actor, []sdk.Msg{&exec})}
		}},
		{"same-block grant then exec of an admin's trip", func(t *testing.T, f *fibreTaintFixture) [][]byte {
			expiration := f.blockTime.Add(time.Hour)
			grant, err := authz.NewMsgGrant(f.addrs[admin], f.addrs[actor],
				authz.NewGenericAuthorization(sdk.MsgTypeURL(&circuittypes.MsgTripCircuitBreaker{})), &expiration)
			require.NoError(t, err)
			exec := authz.NewMsgExec(f.addrs[actor], []sdk.Msg{trip(f.addrs[admin])})
			return [][]byte{f.tx(t, admin, []sdk.Msg{grant}), f.tx(t, actor, []sdk.Msg{&exec})}
		}},
		{"exec of an admin's circuit authorization", func(t *testing.T, f *fibreTaintFixture) [][]byte {
			exec := authz.NewMsgExec(f.addrs[actor], []sdk.Msg{&circuittypes.MsgAuthorizeCircuitBreaker{
				Granter:     f.addrs[admin].String(),
				Grantee:     f.addrs[actor].String(),
				Permissions: &circuittypes.Permissions{Level: circuittypes.Permissions_LEVEL_SUPER_ADMIN},
			}})
			return [][]byte{f.tx(t, actor, []sdk.Msg{&exec})}
		}},
		{"delegated fibre params update", func(t *testing.T, f *fibreTaintFixture) [][]byte {
			gov := authtypes.NewModuleAddress(govtypes.ModuleName)
			expiration := f.blockTime.Add(time.Hour)
			require.NoError(t, f.app.AuthzKeeper.SaveGrant(f.ctx(), f.addrs[actor], gov,
				authz.NewGenericAuthorization(sdk.MsgTypeURL(&fibretypes.MsgUpdateFibreParams{})), &expiration))
			params := f.app.FibreKeeper.GetParams(f.ctx())
			params.PaymentPromiseTimeout = time.Minute
			exec := authz.NewMsgExec(f.addrs[actor], []sdk.Msg{&fibretypes.MsgUpdateFibreParams{Authority: gov.String(), Params: params}})
			return [][]byte{f.tx(t, actor, []sdk.Msg{&exec})}
		}},
		{"exec with an unresolvable signer", func(t *testing.T, f *fibreTaintFixture) [][]byte {
			bogus := &banktypes.MsgSend{FromAddress: "not-bech32", ToAddress: f.addrs[actor].String(), Amount: sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, 1))}
			exec := authz.NewMsgExec(f.addrs[actor], []sdk.Msg{bogus})
			return [][]byte{f.tx(t, actor, []sdk.Msg{&exec})}
		}},
		{"exec of a vote", func(t *testing.T, f *fibreTaintFixture) [][]byte {
			exec := authz.NewMsgExec(f.addrs[actor], []sdk.Msg{govv1.NewMsgVote(f.addrs[other], 999, govv1.OptionYes, "")})
			return [][]byte{f.tx(t, actor, []sdk.Msg{&exec})}
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFibreTaintFixture(t, 3)
			seedFibreEscrow(t, f.app, f.addrs[other], fibretypes.PaymentAmount(100).Amount.Int64())
			require.NoError(t, f.app.CircuitKeeper.Permissions.Set(f.ctx(), f.addrs[admin],
				circuittypes.Permissions{Level: circuittypes.Permissions_LEVEL_SUPER_ADMIN}))
			f.requireOtherPayForFibreDropped(t, tc.attack(t, f), f.pff(t, other))
		})
	}
}

// TestPayForFibreSettlesInBlockAfterUnmodelableMessage checks that an
// unmodelable message only delays pay-for-fibre txs to the next block.
func TestPayForFibreSettlesInBlockAfterUnmodelableMessage(t *testing.T) {
	f := newFibreTaintFixture(t, 2)
	attacker, other := 0, 1
	seedFibreEscrow(t, f.app, f.addrs[other], fibretypes.PaymentAmount(100).Amount.Int64())

	exec := authz.NewMsgExec(f.addrs[attacker], []sdk.Msg{&circuittypes.MsgTripCircuitBreaker{
		Authority:   authtypes.NewModuleAddress(govtypes.ModuleName).String(),
		MsgTypeUrls: []string{sdk.MsgTypeURL(&fibretypes.MsgPayForFibre{})},
	}})
	pff := f.pff(t, other)
	f.requireOtherPayForFibreDropped(t, [][]byte{f.tx(t, attacker, []sdk.Msg{&exec})}, pff)
	_, err := f.app.Commit()
	require.NoError(t, err)

	f.blockTime = f.blockTime.Add(time.Second)
	kept := f.prepare(t, [][]byte{pff})
	require.Equal(t, [][]byte{pff}, kept)
	f.requireNoUnpaidPayForFibre(t, kept)
}

// TestPayForFibreSettlesAfterModelableMessages checks that ordinary user and
// relayer traffic from other accounts does not stop pay-for-fibre settlement.
func TestPayForFibreSettlesAfterModelableMessages(t *testing.T) {
	f := newFibreTaintFixture(t, 2)
	user0, payer := 0, 1
	seedFibreEscrow(t, f.app, f.addrs[payer], fibretypes.PaymentAmount(100).Amount.Int64())

	coin := sdk.NewInt64Coin(appconsts.BondDenom, 1_000)
	vals, err := f.app.StakingKeeper.GetAllValidators(f.ctx())
	require.NoError(t, err)
	require.NotEmpty(t, vals)
	send := f.tx(t, user0, []sdk.Msg{banktypes.NewMsgSend(f.addrs[user0], f.addrs[user0], sdk.NewCoins(coin))})
	delegate := f.tx(t, user0, []sdk.Msg{stakingtypes.NewMsgDelegate(f.addrs[user0].String(), vals[0].OperatorAddress, coin)})
	transfer := f.tx(t, user0, []sdk.Msg{ibctransfertypes.NewMsgTransfer("transfer", "channel-0", coin,
		f.addrs[user0].String(), "cosmos1recipient", clienttypes.NewHeight(1, 1000), 0, "")})
	recv := f.tx(t, user0, []sdk.Msg{f.recvPacket()})
	blob, err := share.NewV0Blob(share.RandomBlobNamespace(), bytes.Repeat([]byte{1}, 100))
	require.NoError(t, err)
	blobTx, _, err := f.signers[user0].CreatePayForBlobs(f.accounts[user0], []*share.Blob{blob}, user.SetGasLimit(1_000_000), user.SetFee(4_000))
	require.NoError(t, err)
	require.NoError(t, f.signers[user0].IncrementSequence(f.accounts[user0]))
	pff := f.pff(t, payer)

	block := [][]byte{send, delegate, transfer, recv, blobTx, pff}
	kept := f.prepare(t, block)
	require.Len(t, kept, len(block))
	require.Contains(t, kept, pff)
	f.requireNoUnpaidPayForFibre(t, kept)
}

// TestPayForFibreAfterRecvPacketWithUnmodelableICAHost checks that an inbound
// IBC packet stops settlement when the ICA host may execute a message the
// branch cannot reason about on behalf of an interchain account.
func TestPayForFibreAfterRecvPacketWithUnmodelableICAHost(t *testing.T) {
	f := newFibreTaintFixture(t, 2)
	relayer, payer := 0, 1
	seedFibreEscrow(t, f.app, f.addrs[payer], fibretypes.PaymentAmount(100).Amount.Int64())
	ctx := f.ctx()
	params := f.app.ICAHostKeeper.GetParams(ctx)
	params.AllowMessages = append(params.AllowMessages, sdk.MsgTypeURL(&authz.MsgExec{}))
	f.app.ICAHostKeeper.SetParams(ctx, params)

	f.requireOtherPayForFibreDropped(t, [][]byte{f.tx(t, relayer, []sdk.Msg{f.recvPacket()})}, f.pff(t, payer))
}

// recvPacket returns a well-formed MsgRecvPacket for a channel that does not
// exist, so it passes message validation and the ante handler and fails only
// when executed.
func (f *fibreTaintFixture) recvPacket() *channeltypes.MsgRecvPacket {
	packet := channeltypes.NewPacket([]byte("data"), 1, "transfer", "channel-0", "transfer", "channel-1", clienttypes.NewHeight(1, 1000), 0)
	return channeltypes.NewMsgRecvPacket(packet, []byte("proof"), clienttypes.NewHeight(1, 1), f.addrs[0].String())
}

// TestCopiedTimeoutDoesNotIterateOwnerWithdrawals checks that judging an
// unexecuted timeout reads no pending withdrawal of its owner, however many
// there are, while still treating a valid timeout as debiting the owner.
func TestCopiedTimeoutDoesNotIterateOwnerWithdrawals(t *testing.T) {
	f := newFibreTaintFixture(t, 2)
	submitter, owner := 0, 1
	payment := fibretypes.PaymentAmount(100).Amount.Int64()
	// All of the escrow is locked in many small pending withdrawals, so a
	// debit has to walk them to cover the payment.
	const withdrawals = 500
	perWithdrawal := payment/withdrawals + 1
	seedFibreEscrow(t, f.app, f.addrs[owner], perWithdrawal*withdrawals)

	ctx := f.ctx()
	ownerStr := f.addrs[owner].String()
	escrow, found := f.app.FibreKeeper.GetEscrowAccount(ctx, ownerStr)
	require.True(t, found)
	escrow.AvailableBalance = sdk.NewInt64Coin(appconsts.BondDenom, 0)
	f.app.FibreKeeper.SetEscrowAccount(ctx, escrow)
	for i := range withdrawals {
		requested := f.blockTime.Add(-time.Duration(i+1) * time.Second)
		f.app.FibreKeeper.SetWithdrawal(ctx, fibretypes.Withdrawal{
			Signer:             ownerStr,
			Amount:             sdk.NewInt64Coin(appconsts.BondDenom, perWithdrawal),
			RequestedTimestamp: requested,
			AvailableTimestamp: f.blockTime.Add(24 * time.Hour),
		})
	}

	send := banktypes.NewMsgSend(f.addrs[submitter], f.addrs[submitter], sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, 1)))
	mixed := f.tx(t, submitter, []sdk.Msg{send, f.timeoutMsg(t, owner, f.addrs[submitter], f.blockTime.Add(-2*time.Hour))})
	pff := f.pff(t, owner)

	var trace bytes.Buffer
	cms := f.app.CommitMultiStore()
	cms.SetTracer(&trace)
	kept := f.prepare(t, [][]byte{mixed, pff})
	resp, err := f.app.ProcessProposal(processProposalRequestAt(t, f.app, [][]byte{mixed, pff}, f.blockTime))
	cms.SetTracer(nil)
	require.NoError(t, err)

	require.NotContains(t, kept, pff, "a valid timeout still marks its owner")
	require.Equal(t, abci.ResponseProcessProposal_REJECT, resp.Status)

	prefix := fibretypes.WithdrawalsBySignerPrefix(ownerStr)
	var traced, withdrawalReads int
	dec := json.NewDecoder(&trace)
	for dec.More() {
		var op struct {
			Key string `json:"key"`
		}
		require.NoError(t, dec.Decode(&op))
		traced++
		key, err := base64.StdEncoding.DecodeString(op.Key)
		require.NoError(t, err)
		if bytes.HasPrefix(key, prefix) {
			withdrawalReads++
		}
	}
	require.Positive(t, traced, "the store trace must capture proposal reads")
	require.Zero(t, withdrawalReads, "the owner's pending withdrawals must not be read")
}

// TestExactGasPayForFibreSettlesWhenModuleBalancesGrow checks that a
// pay-for-fibre whose gas limit only just covers its settlement on the proposal
// branch also settles in FinalizeBlock, where the fibre module and fee
// collector balances it transfers between can be larger: an earlier escrow
// deposit is not executed on the branch, and FinalizeBlock collects every
// tx's fee, including those of later pay-for-fibre txs, before any message.
func TestExactGasPayForFibreSettlesWhenModuleBalancesGrow(t *testing.T) {
	f := newFibreTaintFixture(t, 3)
	depositor, payer, later := 0, 1, 2
	payment := fibretypes.PaymentAmount(100).Amount.Int64()
	seedFibreEscrow(t, f.app, f.addrs[payer], payment)
	seedFibreEscrow(t, f.app, f.addrs[later], payment)

	// Both push a balance past several powers of ten.
	const big = 1_000_000_000
	deposit := f.tx(t, depositor, []sdk.Msg{&fibretypes.MsgDepositToEscrow{
		Signer: f.addrs[depositor].String(), Amount: sdk.NewInt64Coin(appconsts.BondDenom, big),
	}})
	pffWithGas := func(gas uint64) []byte {
		return newSignedPayForFibreTxAt(t, f.signers[payer], f.accounts[payer], true, f.blockTime,
			user.SetGasLimit(gas), user.SetFee(4_000))
	}
	laterPFF := f.pff(t, later, user.SetFee(big))

	// The smallest gas limit with which the branch keeps the pay-for-fibre.
	lo, hi := uint64(1), uint64(1_000_000)
	require.Contains(t, f.prepare(t, [][]byte{deposit, pffWithGas(hi), laterPFF}), pffWithGas(hi))
	for lo < hi {
		mid := (lo + hi) / 2
		if pff := pffWithGas(mid); slices.ContainsFunc(f.prepare(t, [][]byte{deposit, pff, laterPFF}), func(tx []byte) bool { return bytes.Equal(tx, pff) }) {
			hi = mid
		} else {
			lo = mid + 1
		}
	}
	pff := pffWithGas(hi)
	require.NoError(t, f.signers[payer].IncrementSequence(f.accounts[payer]))

	kept := f.prepare(t, [][]byte{deposit, pff, laterPFF})
	require.Equal(t, [][]byte{deposit, pff, laterPFF}, kept)
	f.requireNoUnpaidPayForFibre(t, kept)
}
