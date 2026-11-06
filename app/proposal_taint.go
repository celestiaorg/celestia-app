package app

import (
	"slices"

	storetypes "cosmossdk.io/store/types"
	"github.com/celestiaorg/celestia-app/v11/fibre"
	fibretypes "github.com/celestiaorg/celestia-app/v11/x/fibre/types"
	"github.com/cometbft/cometbft/crypto/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	icatypes "github.com/cosmos/ibc-go/v8/modules/apps/27-interchain-accounts/types"
)

// proposalTaint decides whether the proposal branch can still prove that a
// pay-for-fibre settles in FinalizeBlock.
//
// PrepareProposal and ProcessProposal run every tx's ante handler but execute
// the messages of only pay-for-fibre txs and pure payment-promise-timeout txs.
// FinalizeBlock runs every tx's ante handler first and then every tx's
// messages in block order, so a pay-for-fibre's message there also sees the
// writes of every earlier message the branch skipped. If one of those writes
// changes what the pay-for-fibre reads, it could settle on the branch, put its
// system blob in the square, and then fail in FinalizeBlock, leaving the blob
// unpaid.
//
// The branch only reasons about messages whose types are on
// modelableMsgTypeURLs: types that cannot change fibre params, the circuit
// breaker, the historical validator sets, or anyone's escrow other than their
// signer's. Such a message only acts for the accounts of its tx, which are
// marked touched, and a pay-for-fibre is settled on the branch only when its
// signers, fee payer, fee granter and escrow owner are all untouched. Any other
// message type, including authz MsgExec, which can carry anything, makes the
// branch stop settling pay-for-fibre txs for the rest of the block. That is
// fail-closed: a new or unlisted message type can only delay pay-for-fibre txs
// to the next block, never let one through unpaid.
type proposalTaint struct {
	env proposalTaintEnv

	touched map[string]struct{}
	// unmodelable is set once a kept tx carries a message the branch cannot
	// reason about. No later pay-for-fibre settles in the block.
	unmodelable bool
	// icaHostSafe caches whether every message the ICA host may execute is
	// modelable. The host params can only change through an unmodelable
	// message, so one read per block is enough.
	icaHostSafe *bool
}

// proposalTaintEnv holds the state lookups proposalTaint needs. Each lookup
// reads the proposal branch it is given. A nil lookup makes proposalTaint
// assume the worst for it.
type proposalTaintEnv struct {
	// isInterchainAccount reports whether addr is an interchain account.
	isInterchainAccount func(sdk.Context, sdk.AccAddress) bool
	// icaHostAllowMessages returns the message types the ICA host executes.
	icaHostAllowMessages func(sdk.Context) []string
	// timeoutSettles reports whether msg may settle on ctx, reading a bounded
	// amount of state and writing nothing.
	timeoutSettles func(sdk.Context, *fibretypes.MsgPaymentPromiseTimeout) bool
}

func newProposalTaint(env proposalTaintEnv) *proposalTaint {
	return &proposalTaint{env: env, touched: make(map[string]struct{})}
}

// modelableMsgTypeURLs lists the message types the proposal branch can reason
// about without executing them. A pay-for-fibre's message reads fibre params,
// its escrow, pending withdrawals and processed-promise record, the fibre
// module's balance, the circuit breaker and the historical validator set. Each
// type below writes none of those except its own signer's escrow, which marks
// the signer touched. Credits to other accounts and bank, fee allowance and
// sequence changes do not matter: FinalizeBlock runs every ante handler before
// any message, and the branch mirrors the ante handlers.
//
// Messages signed by a module authority (params, upgrades, community pool
// spends, client recovery, circuit breaker) are deliberately absent.
// MsgRecvPacket is handled separately; see modelable.
var modelableMsgTypeURLs = map[string]struct{}{
	// Blob, fibre and Celestia user messages.
	"/celestia.blob.v1.MsgPayForBlobs":                     {}, // consumes gas only
	"/celestia.fibre.v1.MsgDepositToEscrow":                {}, // credits the signer's own escrow
	"/celestia.fibre.v1.MsgRequestWithdrawal":              {}, // locks the signer's own escrow
	"/celestia.forwarding.v1.MsgForward":                   {}, // moves funds out of a keyless forwarding address
	"/celestia.signal.v1.MsgSignalVersion":                 {}, // records a validator's version signal
	"/celestia.signal.v1.MsgTryUpgrade":                    {}, // schedules an upgrade for a later height
	"/celestia.valaddr.v1.MsgSetFibreProviderInfo":         {}, // validator's provider info, not read by settlement
	"/celestia.zkism.v1.MsgSubmitMessages":                 {}, // delivers hyperlane messages to warp (mints or unlocks)
	"/celestia.zkism.v1.MsgCreateInterchainSecurityModule": {}, // ISM state only
	"/celestia.zkism.v1.MsgUpdateInterchainSecurityModule": {}, // ISM owner, ISM state only
	"/celestia.zkism.v1.MsgTransferOwnership":              {}, // ISM owner, ISM state only
	"/celestia.zkism.v1.MsgUpdateRootHistorySize":          {}, // ISM owner, ISM state only
	"/celestia.zkism.v1.MsgUpdateVerifyingKeys":            {}, // ISM owner, ISM state only

	// Cosmos SDK user messages.
	"/cosmos.authz.v1beta1.MsgGrant":                              {}, // stores a grant; using it needs MsgExec, which is unmodelable
	"/cosmos.authz.v1beta1.MsgRevoke":                             {}, // deletes a grant
	"/cosmos.authz.v1beta1.MsgPruneExpiredGrants":                 {}, // deletes expired grants
	"/cosmos.bank.v1beta1.MsgSend":                                {}, // debits the signer
	"/cosmos.bank.v1beta1.MsgMultiSend":                           {}, // debits its signer
	"/cosmos.distribution.v1beta1.MsgDepositValidatorRewardsPool": {}, // debits the signer
	"/cosmos.distribution.v1beta1.MsgFundCommunityPool":           {}, // debits the signer
	"/cosmos.distribution.v1beta1.MsgSetWithdrawAddress":          {}, // signer's distribution state
	"/cosmos.distribution.v1beta1.MsgWithdrawDelegatorReward":     {}, // pays out to the signer
	"/cosmos.distribution.v1beta1.MsgWithdrawValidatorCommission": {}, // pays out to the signer
	"/cosmos.feegrant.v1beta1.MsgGrantAllowance":                  {}, // allowances are only read by ante handlers
	"/cosmos.feegrant.v1beta1.MsgRevokeAllowance":                 {}, // same
	"/cosmos.feegrant.v1beta1.MsgPruneAllowances":                 {}, // same
	"/cosmos.gov.v1.MsgCancelProposal":                            {}, // proposals execute in EndBlocker
	"/cosmos.gov.v1.MsgDeposit":                                   {}, // same
	"/cosmos.gov.v1.MsgSubmitProposal":                            {}, // same
	"/cosmos.gov.v1.MsgVote":                                      {}, // same
	"/cosmos.gov.v1.MsgVoteWeighted":                              {}, // same
	"/cosmos.gov.v1beta1.MsgDeposit":                              {}, // same
	"/cosmos.gov.v1beta1.MsgSubmitProposal":                       {}, // same
	"/cosmos.gov.v1beta1.MsgVote":                                 {}, // same
	"/cosmos.gov.v1beta1.MsgVoteWeighted":                         {}, // same
	"/cosmos.slashing.v1beta1.MsgUnjail":                          {}, // validator set changes apply in EndBlocker
	"/cosmos.staking.v1beta1.MsgBeginRedelegate":                  {}, // same; historical sets are written in BeginBlocker
	"/cosmos.staking.v1beta1.MsgCancelUnbondingDelegation":        {}, // same
	"/cosmos.staking.v1beta1.MsgCreateValidator":                  {}, // same
	"/cosmos.staking.v1beta1.MsgDelegate":                         {}, // same
	"/cosmos.staking.v1beta1.MsgEditValidator":                    {}, // same
	"/cosmos.staking.v1beta1.MsgUndelegate":                       {}, // same
	"/cosmos.vesting.v1beta1.MsgCreatePeriodicVestingAccount":     {}, // debits the signer
	"/cosmos.vesting.v1beta1.MsgCreatePermanentLockedAccount":     {}, // same
	"/cosmos.vesting.v1beta1.MsgCreateVestingAccount":             {}, // same

	// Hyperlane: core and warp state, payments debit the signer, and inbound
	// messages are delivered to warp, which mints or unlocks.
	"/hyperlane.core.interchain_security.v1.MsgAnnounceValidator":           {},
	"/hyperlane.core.interchain_security.v1.MsgCreateMerkleRootMultisigIsm": {},
	"/hyperlane.core.interchain_security.v1.MsgCreateMessageIdMultisigIsm":  {},
	"/hyperlane.core.interchain_security.v1.MsgCreateNoopIsm":               {},
	"/hyperlane.core.interchain_security.v1.MsgCreateRoutingIsm":            {},
	"/hyperlane.core.interchain_security.v1.MsgRemoveRoutingIsmDomain":      {},
	"/hyperlane.core.interchain_security.v1.MsgSetRoutingIsmDomain":         {},
	"/hyperlane.core.interchain_security.v1.MsgUpdateRoutingIsmOwner":       {},
	"/hyperlane.core.post_dispatch.v1.MsgClaim":                             {},
	"/hyperlane.core.post_dispatch.v1.MsgCreateIgp":                         {},
	"/hyperlane.core.post_dispatch.v1.MsgCreateMerkleTreeHook":              {},
	"/hyperlane.core.post_dispatch.v1.MsgCreateNoopHook":                    {},
	"/hyperlane.core.post_dispatch.v1.MsgPayForGas":                         {},
	"/hyperlane.core.post_dispatch.v1.MsgSetDestinationGasConfig":           {},
	"/hyperlane.core.post_dispatch.v1.MsgSetIgpOwner":                       {},
	"/hyperlane.core.v1.MsgCreateMailbox":                                   {},
	"/hyperlane.core.v1.MsgProcessMessage":                                  {},
	"/hyperlane.core.v1.MsgSetMailbox":                                      {},
	"/hyperlane.warp.v1.MsgCreateCollateralToken":                           {},
	"/hyperlane.warp.v1.MsgCreateNativeSyntheticToken":                      {},
	"/hyperlane.warp.v1.MsgCreateSyntheticToken":                            {},
	"/hyperlane.warp.v1.MsgEnrollRemoteRouter":                              {},
	"/hyperlane.warp.v1.MsgRemoteTransfer":                                  {},
	"/hyperlane.warp.v1.MsgSetToken":                                        {},
	"/hyperlane.warp.v1.MsgUnrollRemoteRouter":                              {},

	// IBC: transfers debit the signer; client, connection and channel
	// messages write IBC state. Acknowledgements and timeouts only refund
	// transfers. Inbound packets are handled in modelable.
	"/ibc.applications.interchain_accounts.host.v1.MsgModuleQuerySafe": {}, // queries only
	"/ibc.applications.transfer.v1.MsgTransfer":                        {},
	"/ibc.core.channel.v1.MsgAcknowledgement":                          {},
	"/ibc.core.channel.v1.MsgChannelCloseConfirm":                      {},
	"/ibc.core.channel.v1.MsgChannelCloseInit":                         {},
	"/ibc.core.channel.v1.MsgChannelOpenAck":                           {},
	"/ibc.core.channel.v1.MsgChannelOpenConfirm":                       {},
	"/ibc.core.channel.v1.MsgChannelOpenInit":                          {},
	"/ibc.core.channel.v1.MsgChannelOpenTry":                           {},
	"/ibc.core.channel.v1.MsgChannelUpgradeAck":                        {},
	"/ibc.core.channel.v1.MsgChannelUpgradeCancel":                     {},
	"/ibc.core.channel.v1.MsgChannelUpgradeConfirm":                    {},
	"/ibc.core.channel.v1.MsgChannelUpgradeInit":                       {},
	"/ibc.core.channel.v1.MsgChannelUpgradeOpen":                       {},
	"/ibc.core.channel.v1.MsgChannelUpgradeTimeout":                    {},
	"/ibc.core.channel.v1.MsgChannelUpgradeTry":                        {},
	"/ibc.core.channel.v1.MsgPruneAcknowledgements":                    {},
	"/ibc.core.channel.v1.MsgTimeout":                                  {},
	"/ibc.core.channel.v1.MsgTimeoutOnClose":                           {},
	"/ibc.core.client.v1.MsgCreateClient":                              {},
	"/ibc.core.client.v1.MsgSubmitMisbehaviour":                        {},
	"/ibc.core.client.v1.MsgUpdateClient":                              {},
	"/ibc.core.client.v1.MsgUpgradeClient":                             {},
	"/ibc.core.connection.v1.MsgConnectionOpenAck":                     {},
	"/ibc.core.connection.v1.MsgConnectionOpenConfirm":                 {},
	"/ibc.core.connection.v1.MsgConnectionOpenInit":                    {},
	"/ibc.core.connection.v1.MsgConnectionOpenTry":                     {},
}

// msgRecvPacketTypeURL is modelable only while the ICA host executes nothing
// but modelable messages: an inbound packet can make the host run them on
// behalf of an interchain account. Transfer packets only credit, and the
// packet-forward middleware only moves funds out of IBC escrow accounts.
const msgRecvPacketTypeURL = "/ibc.core.channel.v1.MsgRecvPacket"

// observeNormalTx updates the taint for a kept non-blob tx that is not a
// pay-for-fibre tx, executing its messages on ctx when the branch can do so
// exactly. Call it after the tx's ante handler succeeded.
func (t *proposalTaint) observeNormalTx(ctx sdk.Context, sdkTx sdk.Tx, execute func(sdk.Context, sdk.Tx) error) {
	if isPureTimeoutTx(sdkTx) && !t.inputsTouched(ctx, sdkTx) {
		// Every input is exact, so the timeouts settle (or fail) on the branch
		// exactly as they will in FinalizeBlock. A failed message keeps the tx
		// (gas only), so the error is ignored.
		_ = execute(ctx, sdkTx)
		return
	}
	t.touch(ctx, sdkTx)
}

// observeBlobTx updates the taint for a kept blob tx. Its MsgPayForBlobs only
// consumes gas, so only a divergent ante handler can make it inexact.
func (t *proposalTaint) observeBlobTx(ctx sdk.Context, sdkTx sdk.Tx) {
	if t.inputsTouched(ctx, sdkTx) {
		t.touch(ctx, sdkTx)
	}
}

// inputsTouched reports whether sdkTx reads an account the branch cannot model
// exactly: a signer, its fee payer or fee granter, or the escrow owner of a
// payment promise it carries.
func (t *proposalTaint) inputsTouched(ctx sdk.Context, sdkTx sdk.Tx) bool {
	if t.unmodelable {
		return true
	}
	sigTx, ok := sdkTx.(authsigning.SigVerifiableTx)
	if !ok {
		return true
	}
	signers, err := sigTx.GetSigners()
	if err != nil {
		return true
	}
	if slices.ContainsFunc(signers, t.isTouched) {
		return true
	}
	if feeTx, ok := sdkTx.(sdk.FeeTx); ok {
		if t.isTouched(feeTx.FeePayer()) {
			return true
		}
		if granter := feeTx.FeeGranter(); len(granter) > 0 {
			if t.isTouched(granter) {
				return true
			}
			// An interchain account can revoke its allowance or spend its
			// balance through an IBC packet the branch does not execute.
			if t.env.isInterchainAccount == nil || t.env.isInterchainAccount(readCtx(ctx), granter) {
				return true
			}
		}
	}
	for _, msg := range sdkTx.GetMsgs() {
		owner, ok, valid := promiseEscrowOwner(msg)
		if !ok {
			continue
		}
		if !valid || t.isTouched(owner) {
			return true
		}
	}
	return false
}

// touch marks the accounts sdkTx acts for, or stops settlement for the rest of
// the block if it carries a message the branch cannot reason about.
func (t *proposalTaint) touch(ctx sdk.Context, sdkTx sdk.Tx) {
	if t.unmodelable {
		return
	}
	sigTx, ok := sdkTx.(authsigning.SigVerifiableTx)
	if !ok {
		// Unreachable for a tx that passed the ante handler, which needs its
		// signers.
		t.unmodelable = true
		return
	}
	signers, err := sigTx.GetSigners()
	if err != nil {
		// Unreachable for the same reason.
		t.unmodelable = true
		return
	}
	for _, s := range signers {
		t.mark(s)
	}
	if feeTx, ok := sdkTx.(sdk.FeeTx); ok {
		t.mark(feeTx.FeePayer())
	}
	for _, msg := range sdkTx.GetMsgs() {
		if m, ok := msg.(*fibretypes.MsgPaymentPromiseTimeout); ok {
			t.touchTimeout(ctx, m)
			continue
		}
		if !t.modelable(ctx, msg) {
			t.unmodelable = true
			return
		}
	}
}

// modelable reports whether the branch can reason about msg without executing
// it; see modelableMsgTypeURLs.
func (t *proposalTaint) modelable(ctx sdk.Context, msg sdk.Msg) bool {
	typeURL := sdk.MsgTypeURL(msg)
	if _, ok := modelableMsgTypeURLs[typeURL]; ok {
		return true
	}
	if typeURL != msgRecvPacketTypeURL {
		return false
	}
	if t.icaHostSafe == nil {
		safe := t.env.icaHostAllowMessages != nil &&
			!slices.ContainsFunc(t.env.icaHostAllowMessages(readCtx(ctx)), func(url string) bool {
				_, ok := modelableMsgTypeURLs[url]
				return !ok
			})
		t.icaHostSafe = &safe
	}
	return *t.icaHostSafe
}

// touchTimeout marks the escrow owner of a timeout the branch does not
// execute, if the timeout would settle. One that would not settle cannot debit
// the escrow in FinalizeBlock either: the escrow and processed-promise records
// it reads only differ from FinalizeBlock for an owner that is already
// touched, and the params only through an unmodelable message.
func (t *proposalTaint) touchTimeout(ctx sdk.Context, msg *fibretypes.MsgPaymentPromiseTimeout) {
	owner, _, valid := promiseEscrowOwner(msg)
	if !valid || t.isTouched(owner) {
		// A malformed key fails the timeout in FinalizeBlock.
		return
	}
	if t.env.timeoutSettles == nil || t.env.timeoutSettles(ctx, msg) {
		t.mark(owner)
	}
}

func (t *proposalTaint) mark(addr []byte) {
	t.touched[string(addr)] = struct{}{}
}

func (t *proposalTaint) isTouched(addr []byte) bool {
	_, ok := t.touched[string(addr)]
	return ok
}

// readCtx returns ctx with an unlimited gas meter, so the taint's own reads
// never run a tx's gas meter out.
func readCtx(ctx sdk.Context) sdk.Context {
	return ctx.WithGasMeter(storetypes.NewInfiniteGasMeter())
}

// isPureTimeoutTx reports whether every message of sdkTx is a
// MsgPaymentPromiseTimeout, the only messages besides MsgPayForFibre that the
// proposal branch executes.
func isPureTimeoutTx(sdkTx sdk.Tx) bool {
	msgs := sdkTx.GetMsgs()
	if len(msgs) == 0 {
		return false
	}
	for _, msg := range msgs {
		if _, ok := msg.(*fibretypes.MsgPaymentPromiseTimeout); !ok {
			return false
		}
	}
	return true
}

// promiseEscrowOwner returns the address of the escrow a payment promise in msg
// settles against. ok is false if msg carries no payment promise; valid is
// false if the promise's signer key is malformed.
func promiseEscrowOwner(msg sdk.Msg) (owner []byte, ok, valid bool) {
	var promise *fibretypes.PaymentPromise
	switch m := msg.(type) {
	case *fibretypes.MsgPayForFibre:
		promise = &m.PaymentPromise
	case *fibretypes.MsgPaymentPromiseTimeout:
		promise = &m.PaymentPromise
	default:
		return nil, false, false
	}
	// secp256k1 PubKey.Address panics on a key of the wrong length.
	if len(promise.SignerPublicKey.Key) != secp256k1.PubKeySize {
		return nil, true, false
	}
	return promise.SignerPublicKey.Address(), true, true
}

// proposalTaintEnv returns the state lookups for a proposalTaint.
func (app *App) proposalTaintEnv() proposalTaintEnv {
	return proposalTaintEnv{
		isInterchainAccount: func(ctx sdk.Context, addr sdk.AccAddress) bool {
			_, ok := app.AccountKeeper.GetAccount(ctx, addr).(*icatypes.InterchainAccount)
			return ok
		},
		icaHostAllowMessages: func(ctx sdk.Context) []string {
			return app.ICAHostKeeper.GetParams(ctx).AllowMessages
		},
		timeoutSettles: func(ctx sdk.Context, msg *fibretypes.MsgPaymentPromiseTimeout) bool {
			// The msg server's checks before it debits the escrow, each a
			// bounded number of reads. The debit itself is not run: it can
			// iterate every pending withdrawal of the owner. A timeout that
			// passes these checks is treated as settling.
			pp := fibre.PaymentPromise{}
			if err := pp.FromProto(&msg.PaymentPromise); err != nil {
				return false
			}
			if err := app.FibreKeeper.ValidatePromiseStateless(&pp); err != nil {
				return false
			}
			expiration, err := app.FibreKeeper.ValidatePaymentPromiseStatefulForTimeout(readCtx(ctx), &msg.PaymentPromise)
			if err != nil {
				return false
			}
			if ctx.BlockTime().Before(expiration) {
				return false
			}
			_, err = pp.Hash()
			return err == nil
		},
	}
}
