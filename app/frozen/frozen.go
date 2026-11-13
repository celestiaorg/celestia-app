// Package frozen restricts accounts whose private keys were exposed: from app
// version 11 their funds can only move to the whitehat recovery address.
package frozen

import (
	"bufio"
	"context"
	_ "embed"
	"fmt"
	"slices"
	"strings"
	"sync"

	errorsmod "cosmossdk.io/errors"
	sdkmath "cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app/v11/app/params"
	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	fibretypes "github.com/celestiaorg/celestia-app/v11/x/fibre/types"
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/bech32"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/cosmos/cosmos-sdk/x/authz"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
)

// ActivationVersion is the first app version that enforces the rules.
const ActivationVersion uint64 = 11

// Whitehat is the only address listed accounts may send funds to.
// TODO: confirm the recovery address before release.
const Whitehat = "celestia1atuap9kfnt6y8ykvn3437e7j3z8qffh33pzt53"

// MaxFee caps the fee a tx with a listed signer may pay, so fees can't drain the account.
var MaxFee = sdk.NewCoins(sdk.NewCoin(appconsts.BondDenom, sdkmath.NewInt(1_000_000)))

// ErrFrozen rejects a tx or transfer that breaks the rules.
var ErrFrozen = errorsmod.Register("frozen", 2, "account is frozen: funds can only move to the whitehat address")

//go:embed addresses.txt
var addressesFile string

var (
	whitehatAddr     = mustDecode(Whitehat)
	feeCollectorAddr = authtypes.NewModuleAddress(authtypes.FeeCollectorName)
)

// DistrKeeper reads a delegator's current withdraw address.
type DistrKeeper interface {
	GetDelegatorWithdrawAddr(ctx context.Context, delAddr sdk.AccAddress) (sdk.AccAddress, error)
}

// Rules holds the frozen list and enforces it as a bank send restriction and an ante decorator.
type Rules struct {
	list  map[string]struct{}
	cdc   codec.Codec
	distr DistrKeeper
}

// NewRules returns rules for the embedded list; cdc resolves signers of messages nested in MsgExec.
func NewRules(cdc codec.Codec, distr DistrKeeper) *Rules {
	return &Rules{list: embeddedList(), cdc: cdc, distr: distr}
}

// embeddedList is parsed once and shared read-only by every Rules.
var embeddedList = sync.OnceValue(func() map[string]struct{} {
	return newSet(parseList(addressesFile))
})

// SetList replaces the list. Only tests should call it.
func (r *Rules) SetList(addrs ...sdk.AccAddress) {
	r.list = newSet(addrs)
}

func newSet(addrs []sdk.AccAddress) map[string]struct{} {
	set := make(map[string]struct{}, len(addrs))
	for _, a := range addrs {
		set[string(a)] = struct{}{}
	}
	return set
}

// Has reports whether addr is listed.
func (r *Rules) Has(addr []byte) bool {
	_, ok := r.list[string(addr)]
	return ok
}

// Active reports whether the rules apply at ctx's app version.
func Active(ctx sdk.Context) bool {
	cp := ctx.ConsensusParams()
	return cp.GetVersion().GetApp() >= ActivationVersion
}

// SendRestriction lets a listed account send only to the whitehat or the fee collector.
func (r *Rules) SendRestriction(ctx context.Context, from, to sdk.AccAddress, _ sdk.Coins) (sdk.AccAddress, error) {
	if !r.Has(from) || to.Equals(whitehatAddr) || to.Equals(feeCollectorAddr) || !Active(sdk.UnwrapSDKContext(ctx)) {
		return to, nil
	}
	return to, ErrFrozen
}

// AnteHandle rejects txs where a listed account signs, grants the fee, funds a
// Fibre payment or withdraws rewards to a third party, unless every message it
// signs is allowed.
func (r *Rules) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	if len(r.list) == 0 || !Active(ctx) {
		return next(ctx, tx, simulate)
	}
	feeTx, ok := tx.(sdk.FeeTx)
	if !ok {
		return ctx, errorsmod.Wrap(ErrFrozen, "tx is not a FeeTx")
	}
	if r.Has(feeTx.FeeGranter()) {
		return ctx, errorsmod.Wrap(ErrFrozen, "listed fee granter")
	}
	sigTx, ok := tx.(authsigning.SigVerifiableTx)
	if !ok {
		return ctx, errorsmod.Wrap(ErrFrozen, "tx is not a SigVerifiableTx")
	}
	signers, err := sigTx.GetSigners()
	if err != nil {
		return ctx, err
	}
	signerListed := slices.ContainsFunc(signers, r.Has)
	if signerListed && !feeTx.GetFee().IsAllLTE(MaxFee) {
		return ctx, errorsmod.Wrapf(ErrFrozen, "fee %s exceeds %s", feeTx.GetFee(), MaxFee)
	}
	for _, msg := range tx.GetMsgs() {
		if signerListed && !allowed(msg) {
			return ctx, errorsmod.Wrapf(ErrFrozen, "%s not allowed", sdk.MsgTypeURL(msg))
		}
		if err := r.checkMsg(ctx, msg); err != nil {
			return ctx, err
		}
	}
	return next(ctx, tx, simulate)
}

// checkMsg rejects Fibre payments from listed escrows, reward withdrawals to a
// third party and, recursively, disallowed messages signed inside MsgExec.
func (r *Rules) checkMsg(ctx sdk.Context, msg sdk.Msg) error {
	switch m := msg.(type) {
	case *distrtypes.MsgWithdrawDelegatorReward:
		return r.checkWithdraw(ctx, m)
	case *fibretypes.MsgPayForFibre:
		return r.checkPromise(&m.PaymentPromise)
	case *fibretypes.MsgPaymentPromiseTimeout:
		return r.checkPromise(&m.PaymentPromise)
	case *authz.MsgExec:
		inner, err := m.GetMessages()
		if err != nil {
			return err
		}
		for _, im := range inner {
			signers, _, err := r.cdc.GetMsgV1Signers(im)
			if err != nil {
				return err
			}
			if !allowed(im) && slices.ContainsFunc(signers, r.Has) {
				return errorsmod.Wrapf(ErrFrozen, "%s not allowed in MsgExec", sdk.MsgTypeURL(im))
			}
			if err := r.checkMsg(ctx, im); err != nil {
				return err
			}
		}
	}
	return nil
}

// checkWithdraw lets a listed delegator withdraw only to itself or the whitehat.
// It reads pre-tx state, so a withdraw address set in the same tx doesn't count.
func (r *Rules) checkWithdraw(ctx sdk.Context, m *distrtypes.MsgWithdrawDelegatorReward) error {
	del, err := sdk.AccAddressFromBech32(m.DelegatorAddress)
	if err != nil || !r.Has(del) {
		return nil // the handler rejects a malformed address
	}
	withdrawAddr, err := r.distr.GetDelegatorWithdrawAddr(ctx, del)
	if err != nil {
		return err
	}
	if !withdrawAddr.Equals(del) && !withdrawAddr.Equals(whitehatAddr) {
		return errorsmod.Wrap(ErrFrozen, "withdraw address is neither the delegator nor the whitehat")
	}
	return nil
}

func (r *Rules) checkPromise(p *fibretypes.PaymentPromise) error {
	// Address panics on a malformed key, and ProcessProposal skips msg ValidateBasic.
	if len(p.SignerPublicKey.Key) != secp256k1.PubKeySize {
		return errorsmod.Wrap(sdkerrors.ErrInvalidPubKey, "payment promise signer key")
	}
	if r.Has(p.SignerPublicKey.Address()) {
		return errorsmod.Wrap(ErrFrozen, "payment promise from a listed escrow")
	}
	return nil
}

// allowed is what a listed account may still sign; SendRestriction pins MsgSend's recipient.
func allowed(msg sdk.Msg) bool {
	switch m := msg.(type) {
	case *banktypes.MsgSend, *stakingtypes.MsgUndelegate, *distrtypes.MsgWithdrawDelegatorReward:
		return true
	case *distrtypes.MsgSetWithdrawAddress:
		return m.WithdrawAddress == Whitehat
	}
	return false
}

func parseList(s string) []sdk.AccAddress {
	var out []sdk.AccAddress
	sc := bufio.NewScanner(strings.NewReader(s))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, mustDecode(line))
	}
	if err := sc.Err(); err != nil {
		panic(err)
	}
	return out
}

// mustDecode avoids sdk.AccAddressFromBech32, whose prefix may not be configured at package init.
func mustDecode(s string) sdk.AccAddress {
	hrp, bz, err := bech32.DecodeAndConvert(s)
	if err != nil || hrp != params.Bech32PrefixAccAddr || (len(bz) != 20 && len(bz) != 32) {
		panic(fmt.Sprintf("frozen: invalid address %q: %v", s, err))
	}
	return bz
}
