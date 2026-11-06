package app

import (
	"bytes"
	"errors"
	"slices"

	"cosmossdk.io/collections"
	storetypes "cosmossdk.io/store/types"
	circuittypes "cosmossdk.io/x/circuit/types"
	"github.com/celestiaorg/celestia-app/v11/fibre"
	fibretypes "github.com/celestiaorg/celestia-app/v11/x/fibre/types"
	"github.com/cometbft/cometbft/crypto/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	"github.com/cosmos/cosmos-sdk/x/authz"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	govv1beta1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1beta1"
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
// Funds, allowances and escrow only shrink with the authorization of the
// account they belong to: as a tx signer, as the signer of a message wrapped in
// authz MsgExec under an existing grant, or, for a keyless interchain account,
// through an IBC packet. An escrow also shrinks through a valid payment promise
// timeout, and a circuit breaker admin can disable MsgPayForFibre altogether.
// FinalizeBlock runs the ante handlers of all txs before any tx's messages,
// so a message sees the writes of every earlier message in the block,
// including grants and circuit breaker permissions the branch never sees.
// Lookups below that read the branch account for that.
//
// So the accounts an unexecuted tx can act for are marked touched (only those
// it can really act for, so a tx cannot taint an unrelated account), interchain
// accounts are always treated as touched, and a pay-for-fibre is only settled
// on the branch when its signers, fee payer, fee granter and escrow owner are
// all untouched. Credits the branch misses only leave FinalizeBlock with more
// funds than the branch saw, which is safe.
type proposalTaint struct {
	env proposalTaintEnv

	touched map[string]struct{}
	// all is set when the branch can no longer vouch for any account, e.g. a
	// circuit breaker admin's message that can disable MsgPayForFibre in
	// FinalizeBlock.
	all bool
}

// proposalTaintEnv holds the state lookups proposalTaint needs. Each lookup
// reads the proposal branch it is given. A nil lookup makes proposalTaint
// assume the worst for it.
type proposalTaintEnv struct {
	// msgSigners returns the v1 signers of a message.
	msgSigners func(sdk.Msg) ([][]byte, error)
	// isInterchainAccount reports whether addr is an interchain account.
	isInterchainAccount func(sdk.Context, sdk.AccAddress) bool
	// hasGrant reports whether granter has an unexpired authz grant to grantee
	// for msgTypeURL.
	hasGrant func(ctx sdk.Context, grantee, granter sdk.AccAddress, msgTypeURL string) bool
	// isCircuitAdmin reports whether addr may trip circuit breakers or grant
	// that permission.
	isCircuitAdmin func(sdk.Context, sdk.AccAddress) bool
	// timeoutSettles reports whether msg may settle on ctx, reading a bounded
	// amount of state and writing nothing.
	timeoutSettles func(sdk.Context, *fibretypes.MsgPaymentPromiseTimeout) bool
}

func newProposalTaint(env proposalTaintEnv) *proposalTaint {
	return &proposalTaint{env: env, touched: make(map[string]struct{})}
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

// touch marks every account the unexecuted messages of sdkTx can act for.
func (t *proposalTaint) touch(ctx sdk.Context, sdkTx sdk.Tx) {
	if t.all {
		return
	}
	sigTx, ok := sdkTx.(authsigning.SigVerifiableTx)
	if !ok {
		// Unreachable for a tx that passed the ante handler, which needs its
		// signers.
		t.all = true
		return
	}
	signers, err := sigTx.GetSigners()
	if err != nil {
		// Unreachable for the same reason.
		t.all = true
		return
	}
	for _, s := range signers {
		t.mark(s)
	}
	if feeTx, ok := sdkTx.(sdk.FeeTx); ok {
		t.mark(feeTx.FeePayer())
	}
	for _, msg := range sdkTx.GetMsgs() {
		switch m := msg.(type) {
		case *authz.MsgExec:
			t.touchExec(ctx, m)
		case *fibretypes.MsgPaymentPromiseTimeout:
			t.touchTimeout(ctx, m)
		default:
			t.touchCircuit(ctx, msg)
		}
		if t.all {
			return
		}
	}
}

// touchExec marks the granters exec can act for: the signer of each wrapped
// message that holds an unexpired grant to the grantee for it. Authz refuses
// the others in FinalizeBlock, and a wrapped message whose signer the codec
// cannot resolve fails the whole MsgExec there, so it acts for no one. The
// ante handler rejects wrapped MsgExec and fibre messages.
func (t *proposalTaint) touchExec(ctx sdk.Context, exec *authz.MsgExec) {
	if t.env.msgSigners == nil {
		t.all = true
		return
	}
	outer, err := t.env.msgSigners(exec)
	if err != nil || len(outer) != 1 {
		return
	}
	grantee := sdk.AccAddress(outer[0])
	nested, err := exec.GetMessages()
	if err != nil {
		return
	}
	for _, msg := range nested {
		// A grant made by an earlier tx in the block exists in FinalizeBlock but
		// not on the branch, so a circuit admin's wrapped message counts
		// whether or not the branch sees a grant for it.
		t.touchCircuit(ctx, msg)
		if t.all {
			return
		}
		if cannotDebitSigner(msg) {
			continue
		}
		signers, err := t.env.msgSigners(msg)
		if err != nil || len(signers) != 1 {
			continue
		}
		granter := sdk.AccAddress(signers[0])
		if bytes.Equal(granter, grantee) {
			continue
		}
		// Without a grant on the branch, the only way FinalizeBlock can have
		// one is an earlier MsgGrant in the block, which the granter signs
		// directly (marking it) or through an exec of a grant the branch sees.
		if t.env.hasGrant != nil && !t.env.hasGrant(readCtx(ctx), grantee, granter, sdk.MsgTypeURL(msg)) {
			continue
		}
		t.mark(granter)
	}
}

// cannotDebitSigner reports whether msg is of a type that, executed on behalf
// of its signer, cannot change the signer's bank balance, fee allowances,
// escrow or circuit breaker state. Keep this list small and obviously safe:
// any type not on it marks its signer.
func cannotDebitSigner(msg sdk.Msg) bool {
	switch msg.(type) {
	case *govv1.MsgVote, *govv1.MsgVoteWeighted, *govv1beta1.MsgVote, *govv1beta1.MsgVoteWeighted:
		// A vote only records the voter's choice on a proposal.
		return true
	}
	return false
}

// touchTimeout marks the escrow owner of a timeout the branch does not
// execute, if the timeout would settle. One that would not settle cannot debit
// the escrow in FinalizeBlock either: the escrow, the processed-promise records
// and the params it reads only differ from FinalizeBlock for an owner that is
// already touched.
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

// touchCircuit taints every account if msg lets a circuit breaker admin
// disable messages, which could disable MsgPayForFibre in FinalizeBlock. Only
// admins can: the circuit module refuses the message from anyone else. A new
// admin can only be made by an admin's MsgAuthorizeCircuitBreaker, which
// taints every account itself, so the branch's view of who is an admin is
// enough. Resetting a breaker only re-enables messages, so it is ignored.
func (t *proposalTaint) touchCircuit(ctx sdk.Context, msg sdk.Msg) {
	switch msg.(type) {
	case *circuittypes.MsgTripCircuitBreaker, *circuittypes.MsgAuthorizeCircuitBreaker:
	default:
		return
	}
	if t.env.msgSigners == nil || t.env.isCircuitAdmin == nil {
		t.all = true
		return
	}
	signers, err := t.env.msgSigners(msg)
	if err != nil {
		return
	}
	for _, s := range signers {
		if t.env.isCircuitAdmin(readCtx(ctx), s) {
			t.all = true
			return
		}
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
		msgSigners: func(msg sdk.Msg) ([][]byte, error) {
			signers, _, err := app.encodingConfig.Codec.GetMsgV1Signers(msg)
			return signers, err
		},
		isInterchainAccount: func(ctx sdk.Context, addr sdk.AccAddress) bool {
			_, ok := app.AccountKeeper.GetAccount(ctx, addr).(*icatypes.InterchainAccount)
			return ok
		},
		hasGrant: func(ctx sdk.Context, grantee, granter sdk.AccAddress, msgTypeURL string) bool {
			auth, _ := app.AuthzKeeper.GetAuthorization(ctx, grantee, granter, msgTypeURL)
			return auth != nil
		},
		isCircuitAdmin: func(ctx sdk.Context, addr sdk.AccAddress) bool {
			if bytes.Equal(addr, app.CircuitKeeper.GetAuthority()) {
				return true
			}
			perms, err := app.CircuitKeeper.Permissions.Get(ctx, addr)
			if errors.Is(err, collections.ErrNotFound) {
				return false
			}
			// Treat a read error as an admin, the safe side.
			return err != nil || perms.Level != circuittypes.Permissions_LEVEL_NONE_UNSPECIFIED
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
