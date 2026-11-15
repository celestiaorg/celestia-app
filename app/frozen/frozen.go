// Package frozen restricts accounts whose private keys were exposed: from app
// version 11 their funds can only move to the whitehat recovery address. Each
// chain ID has its own list and whitehat; other chains have no list.
package frozen

import (
	"bufio"
	"bytes"
	"context"
	"embed"
	"fmt"
	"maps"
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

// The whitehat is the only address listed accounts may send funds to.
const (
	// TODO: confirm the recovery address before release.
	MainnetWhitehat = "celestia1atuap9kfnt6y8ykvn3437e7j3z8qffh33pzt53"
	MochaWhitehat   = "celestia1xk4tasrhcr3w6jyq4xzd8x6aapv9hmfuh8mcxn" // test key
	CortoWhitehat   = "celestia1yny4dz396zd2vp85fnqynnnq6pnj00lz95xv09" // test key
)

// whitehats lists the chains with a list; each reads addresses/<chain ID>.txt.
var whitehats = map[string]string{
	appconsts.MainnetChainID: MainnetWhitehat,
	appconsts.MochaChainID:   MochaWhitehat,
	appconsts.CortoChainID:   CortoWhitehat,
}

// MaxFee caps the fee a tx with a listed signer may pay, so fees can't drain the account.
var MaxFee = sdk.NewCoins(sdk.NewCoin(appconsts.BondDenom, sdkmath.NewInt(1_000_000)))

// ErrFrozen rejects a tx or transfer that breaks the rules.
var ErrFrozen = errorsmod.Register("frozen", 2, "account is frozen: funds can only move to the whitehat address")

//go:embed addresses/*.txt
var addressFiles embed.FS

var feeCollectorAddr = authtypes.NewModuleAddress(authtypes.FeeCollectorName)

// network is the list and whitehat in force on one chain.
type network struct {
	list        map[string]struct{}
	whitehat    sdk.AccAddress
	whitehatStr string
}

func newNetwork(whitehat string, addrs []sdk.AccAddress) *network {
	return &network{list: newSet(addrs), whitehat: mustDecode(whitehat), whitehatStr: whitehat}
}

// has reports whether addr is listed; a nil network lists nothing.
func (n *network) has(addr []byte) bool {
	if n == nil {
		return false
	}
	_, ok := n.list[string(addr)]
	return ok
}

// DistrKeeper reads a delegator's current withdraw address.
type DistrKeeper interface {
	GetDelegatorWithdrawAddr(ctx context.Context, delAddr sdk.AccAddress) (sdk.AccAddress, error)
}

// Rules holds the per-chain lists and enforces them as a bank send restriction and an ante decorator.
type Rules struct {
	networks map[string]*network // by chain ID
	cdc      codec.Codec
	distr    DistrKeeper
}

// NewRules returns rules for the embedded lists; cdc resolves signers of messages nested in MsgExec.
func NewRules(cdc codec.Codec, distr DistrKeeper) *Rules {
	return &Rules{networks: embeddedNetworks(), cdc: cdc, distr: distr}
}

// embeddedNetworks is parsed once and shared read-only by every Rules.
var embeddedNetworks = sync.OnceValue(func() map[string]*network {
	networks := make(map[string]*network, len(whitehats))
	for chainID, whitehat := range whitehats {
		bz, err := addressFiles.ReadFile("addresses/" + chainID + ".txt")
		if err != nil {
			panic(err)
		}
		networks[chainID] = newNetwork(whitehat, parseList(string(bz)))
	}
	return networks
})

// SetNetwork sets chainID's list and whitehat. Only tests should call it.
func (r *Rules) SetNetwork(chainID, whitehat string, addrs ...sdk.AccAddress) {
	networks := maps.Clone(r.networks)
	networks[chainID] = newNetwork(whitehat, addrs)
	r.networks = networks
}

func newSet(addrs []sdk.AccAddress) map[string]struct{} {
	set := make(map[string]struct{}, len(addrs))
	for _, a := range addrs {
		set[string(a)] = struct{}{}
	}
	return set
}

// Has reports whether addr is listed on chainID.
func (r *Rules) Has(chainID string, addr []byte) bool {
	return r.networks[chainID].has(addr)
}

// Active reports whether the rules apply at ctx's app version.
func Active(ctx sdk.Context) bool {
	cp := ctx.ConsensusParams()
	return cp.GetVersion().GetApp() >= ActivationVersion
}

// SendRestriction lets a listed account send only to the whitehat or the fee collector.
func (r *Rules) SendRestriction(ctx context.Context, from, to sdk.AccAddress, _ sdk.Coins) (sdk.AccAddress, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	n := r.networks[sdkCtx.ChainID()]
	if !n.has(from) || bytes.Equal(to, n.whitehat) || bytes.Equal(to, feeCollectorAddr) || !Active(sdkCtx) {
		return to, nil
	}
	return to, ErrFrozen
}

// AnteHandle rejects txs where a listed account signs, grants the fee, funds a
// Fibre payment or withdraws rewards to a third party, unless every message it
// signs is allowed.
func (r *Rules) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	n := r.networks[ctx.ChainID()]
	if n == nil || len(n.list) == 0 || !Active(ctx) {
		return next(ctx, tx, simulate)
	}
	feeTx, ok := tx.(sdk.FeeTx)
	if !ok {
		return ctx, errorsmod.Wrap(ErrFrozen, "tx is not a FeeTx")
	}
	if n.has(feeTx.FeeGranter()) {
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
	signerListed := slices.ContainsFunc(signers, n.has)
	if signerListed && !feeTx.GetFee().IsAllLTE(MaxFee) {
		return ctx, errorsmod.Wrapf(ErrFrozen, "fee %s exceeds %s", feeTx.GetFee(), MaxFee)
	}
	for _, msg := range tx.GetMsgs() {
		if signerListed && !n.allowed(msg) {
			return ctx, errorsmod.Wrapf(ErrFrozen, "%s not allowed", sdk.MsgTypeURL(msg))
		}
		if err := r.checkMsg(ctx, n, msg); err != nil {
			return ctx, err
		}
	}
	return next(ctx, tx, simulate)
}

// checkMsg rejects Fibre payments from listed escrows, reward withdrawals to a
// third party and, recursively, disallowed messages signed inside MsgExec.
func (r *Rules) checkMsg(ctx sdk.Context, n *network, msg sdk.Msg) error {
	switch m := msg.(type) {
	case *distrtypes.MsgWithdrawDelegatorReward:
		return r.checkWithdraw(ctx, n, m)
	case *fibretypes.MsgPayForFibre:
		return n.checkPromise(&m.PaymentPromise)
	case *fibretypes.MsgPaymentPromiseTimeout:
		return n.checkPromise(&m.PaymentPromise)
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
			if !n.allowed(im) && slices.ContainsFunc(signers, n.has) {
				return errorsmod.Wrapf(ErrFrozen, "%s not allowed in MsgExec", sdk.MsgTypeURL(im))
			}
			if err := r.checkMsg(ctx, n, im); err != nil {
				return err
			}
		}
	}
	return nil
}

// checkWithdraw lets a listed delegator withdraw only to itself or the whitehat.
// It reads pre-tx state, so a withdraw address set in the same tx doesn't count.
func (r *Rules) checkWithdraw(ctx sdk.Context, n *network, m *distrtypes.MsgWithdrawDelegatorReward) error {
	del, err := sdk.AccAddressFromBech32(m.DelegatorAddress)
	if err != nil || !n.has(del) {
		return nil // the handler rejects a malformed address
	}
	withdrawAddr, err := r.distr.GetDelegatorWithdrawAddr(ctx, del)
	if err != nil {
		return err
	}
	if !bytes.Equal(withdrawAddr, del) && !bytes.Equal(withdrawAddr, n.whitehat) {
		return errorsmod.Wrap(ErrFrozen, "withdraw address is neither the delegator nor the whitehat")
	}
	return nil
}

func (n *network) checkPromise(p *fibretypes.PaymentPromise) error {
	// Address panics on a malformed key, and ProcessProposal skips msg ValidateBasic.
	if len(p.SignerPublicKey.Key) != secp256k1.PubKeySize {
		return errorsmod.Wrap(sdkerrors.ErrInvalidPubKey, "payment promise signer key")
	}
	if n.has(p.SignerPublicKey.Address()) {
		return errorsmod.Wrap(ErrFrozen, "payment promise from a listed escrow")
	}
	return nil
}

// allowed is what a listed account may still sign; SendRestriction pins MsgSend's recipient.
func (n *network) allowed(msg sdk.Msg) bool {
	switch m := msg.(type) {
	case *banktypes.MsgSend, *stakingtypes.MsgUndelegate, *distrtypes.MsgWithdrawDelegatorReward:
		return true
	case *distrtypes.MsgSetWithdrawAddress:
		return m.WithdrawAddress == n.whitehatStr
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
