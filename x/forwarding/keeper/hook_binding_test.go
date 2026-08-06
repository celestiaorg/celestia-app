package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	"github.com/bcp-innovations/hyperlane-cosmos/util"
	"github.com/celestiaorg/celestia-app/v10/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v10/x/forwarding/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

// otherHookID is a second valid hook id, distinct from customHookID, used to check that
// an address bound to one hook cannot be forwarded through another.
const otherHookID = "0x726f757465725f706f73745f6469737061746368000000040000000000000011"

// fundForwardAddr puts a deposit at the setup's forwarding address and gives the signer
// enough to cover the IGP fee, without asserting anything about the outcome.
func fundForwardAddr(s *testIGPSetup) {
	s.bankKeeper.Balances[s.forwardAddr.String()] = sdk.NewCoins(sdk.NewCoin(appconsts.BondDenom, math.NewInt(1000)))
	s.bankKeeper.Balances[s.signer.String()] = sdk.NewCoins(sdk.NewCoin(appconsts.BondDenom, math.NewInt(500)))
	s.hyperlaneKeeper.QuotedFee = sdk.NewCoins(sdk.NewCoin(appconsts.BondDenom, math.NewInt(100)))
}

// The griefing case this binding exists to prevent: a deposit sitting at a default-hook
// address cannot be pushed through a hook the depositor never chose. Previously any
// caller could name a free hook here and strand the deposit without funding delivery.
func TestForward_CustomHookOnDefaultAddress_Rejected(t *testing.T) {
	s := newTestIGPSetup(t)
	fundForwardAddr(s)

	msg := types.NewMsgForward(
		s.signer.String(), s.forwardAddr.String(), s.destDomain, s.destRecipient, s.tokenID,
		sdk.NewCoin(appconsts.BondDenom, math.NewInt(100)),
	)
	msg.CustomHookId = customHookID

	_, err := s.msgServer.Forward(s.ctx, msg)
	require.ErrorIs(t, err, types.ErrAddressMismatch)

	// Rejected before anything moved: the deposit is intact and no warp dispatch happened.
	require.Equal(t, math.NewInt(1000), s.bankKeeper.GetBalance(s.ctx, s.forwardAddr, appconsts.BondDenom).Amount)
	require.Nil(t, s.warpKeeper.CapturedHookId)
}

// An address bound to one hook cannot be forwarded through a different one.
func TestForward_HookBoundAddress_WrongHookRejected(t *testing.T) {
	s := newTestIGPSetup(t)
	s.useHookBoundAddress(t, customHookID)
	fundForwardAddr(s)

	msg := types.NewMsgForward(
		s.signer.String(), s.forwardAddr.String(), s.destDomain, s.destRecipient, s.tokenID,
		sdk.NewCoin(appconsts.BondDenom, math.NewInt(100)),
	)
	msg.CustomHookId = otherHookID

	_, err := s.msgServer.Forward(s.ctx, msg)
	require.ErrorIs(t, err, types.ErrAddressMismatch)
	require.Equal(t, math.NewInt(1000), s.bankKeeper.GetBalance(s.ctx, s.forwardAddr, appconsts.BondDenom).Amount)
}

// The binding holds in the other direction too: an address bound to a hook cannot be
// downgraded to the mailbox default hook by omitting custom_hook_id.
func TestForward_HookBoundAddress_OmittedHookRejected(t *testing.T) {
	s := newTestIGPSetup(t)
	s.useHookBoundAddress(t, customHookID)
	fundForwardAddr(s)

	msg := types.NewMsgForward(
		s.signer.String(), s.forwardAddr.String(), s.destDomain, s.destRecipient, s.tokenID,
		sdk.NewCoin(appconsts.BondDenom, math.NewInt(100)),
	)
	// CustomHookId intentionally left empty.

	_, err := s.msgServer.Forward(s.ctx, msg)
	require.ErrorIs(t, err, types.ErrAddressMismatch)
	require.Equal(t, math.NewInt(1000), s.bankKeeper.GetBalance(s.ctx, s.forwardAddr, appconsts.BondDenom).Amount)
}

// A zero-address custom_hook_id still means "mailbox default hook", so it must resolve to
// the default-hook address rather than being treated as a hook to bind against.
func TestForward_ZeroHookResolvesToDefaultAddress(t *testing.T) {
	s := newTestIGPSetup(t)
	setupSuccessfulForward(s)

	msg := types.NewMsgForward(
		s.signer.String(), s.forwardAddr.String(), s.destDomain, s.destRecipient, s.tokenID,
		sdk.NewCoin(appconsts.BondDenom, math.NewInt(100)),
	)
	msg.CustomHookId = util.NewZeroAddress().String()

	// s.forwardAddr is the default-hook (v1) address, so this must succeed.
	resp, err := s.msgServer.Forward(s.ctx, msg)
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Nil(t, s.warpKeeper.CapturedHookId, "zero hook must dispatch through the mailbox default")
}
