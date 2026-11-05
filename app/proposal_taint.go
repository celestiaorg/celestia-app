package app

import (
	"slices"

	storetypes "cosmossdk.io/store/types"
	circuittypes "cosmossdk.io/x/circuit/types"
	fibretypes "github.com/celestiaorg/celestia-app/v11/x/fibre/types"
	"github.com/cometbft/cometbft/crypto/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	"github.com/cosmos/cosmos-sdk/x/authz"
	icatypes "github.com/cosmos/ibc-go/v8/modules/apps/27-interchain-accounts/types"
)

// proposalTaint tracks which accounts the proposal branch can no longer model
// exactly.
//
// PrepareProposal and ProcessProposal run every tx's ante handler but execute
// the messages of only pay-for-fibre txs and pure payment-promise-timeout txs.
// Any other message (a bank send, a feegrant revocation, an escrow deposit, an
// authz exec, ...) runs only in FinalizeBlock, so the branch can disagree with
// FinalizeBlock about the accounts those messages act for: their bank balance,
// their fee allowances, their escrow, and, if a tx's ante handler then fails in
// FinalizeBlock, their sequence. A pay-for-fibre that depends on such an
// account could settle on the branch, put its system blob in the square, and
// then fail in FinalizeBlock, leaving the blob unpaid.
//
// Funds and allowances only move with the authorization of the account they
// belong to: as a tx signer, as the signer of a message wrapped in authz
// MsgExec, or, for a keyless interchain account, through an IBC packet. So
// every account that signs a message the branch does not execute is marked
// touched, interchain accounts are always treated as touched, and a
// pay-for-fibre is only settled on the branch when its signers, fee payer, fee
// granter and escrow owner are all untouched. Credits the branch misses only
// leave FinalizeBlock with more funds than the branch saw, which is safe.
type proposalTaint struct {
	// msgSigners returns the signers of a message wrapped in authz MsgExec. When
	// nil, any MsgExec taints every account.
	msgSigners func(sdk.Msg) ([][]byte, error)
	// isInterchainAccount reports whether addr is an interchain account. When
	// nil, no account is treated as one.
	isInterchainAccount func(sdk.Context, sdk.AccAddress) bool

	touched map[string]struct{}
	// all is set when the branch can no longer vouch for any account, e.g. a
	// circuit breaker message that can disable MsgPayForFibre in FinalizeBlock.
	all bool
}

func newProposalTaint(msgSigners func(sdk.Msg) ([][]byte, error), isInterchainAccount func(sdk.Context, sdk.AccAddress) bool) *proposalTaint {
	return &proposalTaint{
		msgSigners:          msgSigners,
		isInterchainAccount: isInterchainAccount,
		touched:             make(map[string]struct{}),
	}
}

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
	t.touch(sdkTx)
}

// observeBlobTx updates the taint for a kept blob tx. Its MsgPayForBlobs only
// consumes gas, so only a divergent ante handler can make it inexact.
func (t *proposalTaint) observeBlobTx(ctx sdk.Context, sdkTx sdk.Tx) {
	if t.inputsTouched(ctx, sdkTx) {
		t.touch(sdkTx)
	}
}

// inputsTouched reports whether sdkTx reads an account the branch cannot model
// exactly: a signer, its fee payer or fee granter, or the escrow owner of a
// payment promise it carries.
func (t *proposalTaint) inputsTouched(ctx sdk.Context, sdkTx sdk.Tx) bool {
	if t.all {
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
			if t.isInterchainAccount != nil && t.isInterchainAccount(ctx.WithGasMeter(storetypes.NewInfiniteGasMeter()), granter) {
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

// touch marks every account sdkTx acts for as touched.
func (t *proposalTaint) touch(sdkTx sdk.Tx) {
	if t.all {
		return
	}
	sigTx, ok := sdkTx.(authsigning.SigVerifiableTx)
	if !ok {
		t.all = true
		return
	}
	signers, err := sigTx.GetSigners()
	if err != nil {
		t.all = true
		return
	}
	for _, s := range signers {
		t.touched[string(s)] = struct{}{}
	}
	if feeTx, ok := sdkTx.(sdk.FeeTx); ok {
		t.touched[string(feeTx.FeePayer())] = struct{}{}
	}
	for _, msg := range sdkTx.GetMsgs() {
		switch m := msg.(type) {
		case *authz.MsgExec:
			if !t.touchExec(m) {
				t.all = true
				return
			}
		case *circuittypes.MsgAuthorizeCircuitBreaker,
			*circuittypes.MsgTripCircuitBreaker,
			*circuittypes.MsgResetCircuitBreaker:
			t.all = true
			return
		}
		owner, ok, valid := promiseEscrowOwner(msg)
		if !ok {
			continue
		}
		if !valid {
			t.all = true
			return
		}
		t.touched[string(owner)] = struct{}{}
	}
}

// touchExec marks the signers of the messages wrapped in exec. It returns false
// if they cannot be determined.
func (t *proposalTaint) touchExec(exec *authz.MsgExec) bool {
	if t.msgSigners == nil {
		return false
	}
	nested, err := exec.GetMessages()
	if err != nil {
		return false
	}
	for _, msg := range nested {
		signers, err := t.msgSigners(msg)
		if err != nil {
			return false
		}
		for _, s := range signers {
			t.touched[string(s)] = struct{}{}
		}
	}
	return true
}

func (t *proposalTaint) isTouched(addr []byte) bool {
	_, ok := t.touched[string(addr)]
	return ok
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

// isInterchainAccountFn returns a lookup that reports whether an address holds
// an interchain account.
func (app *App) isInterchainAccountFn() func(sdk.Context, sdk.AccAddress) bool {
	return func(ctx sdk.Context, addr sdk.AccAddress) bool {
		_, ok := app.AccountKeeper.GetAccount(ctx, addr).(*icatypes.InterchainAccount)
		return ok
	}
}

// msgSignersFn returns the v1 signers of a message.
func (app *App) msgSignersFn() func(sdk.Msg) ([][]byte, error) {
	return func(msg sdk.Msg) ([][]byte, error) {
		signers, _, err := app.encodingConfig.Codec.GetMsgV1Signers(msg)
		return signers, err
	}
}
