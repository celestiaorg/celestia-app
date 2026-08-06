package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
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

// Metadata is committed too, so it cannot be substituted on an address that does not
// commit to it. Without this, a caller could attach arbitrary metadata to someone else's
// default-hook deposit.
func TestForward_MetadataOnDefaultAddress_Rejected(t *testing.T) {
	s := newTestIGPSetup(t)
	fundForwardAddr(s)

	msg := types.NewMsgForward(
		s.signer.String(), s.forwardAddr.String(), s.destDomain, s.destRecipient, s.tokenID,
		sdk.NewCoin(appconsts.BondDenom, math.NewInt(100)),
	)
	msg.CustomHookMetadata = "0xabcdef"

	_, err := s.msgServer.Forward(s.ctx, msg)
	require.ErrorIs(t, err, types.ErrAddressMismatch)
	require.Equal(t, math.NewInt(1000), s.bankKeeper.GetBalance(s.ctx, s.forwardAddr, appconsts.BondDenom).Amount)
}

// An address may commit to metadata alone, meaning the mailbox default hook with that
// exact metadata. Such an address still cannot be forwarded with the metadata omitted.
func TestForward_MetadataOnlyBinding(t *testing.T) {
	s := newTestIGPSetup(t)
	s.useHookBoundAddressWithMetadata(t, "", "0xabcdef")
	setupSuccessfulForward(s)

	newMsg := func() *types.MsgForward {
		return types.NewMsgForward(
			s.signer.String(), s.forwardAddr.String(), s.destDomain, s.destRecipient, s.tokenID,
			sdk.NewCoin(appconsts.BondDenom, math.NewInt(100)),
		)
	}

	// Omitting the committed metadata is rejected.
	_, err := s.msgServer.Forward(s.ctx, newMsg())
	require.ErrorIs(t, err, types.ErrAddressMismatch)

	// Supplying it succeeds, and dispatch still uses the mailbox default hook.
	msg := newMsg()
	msg.CustomHookMetadata = "0xabcdef"
	resp, err := s.msgServer.Forward(s.ctx, msg)
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Nil(t, s.warpKeeper.CapturedHookId, "metadata-only binding must still dispatch through the default hook")
}

// A hook-and-metadata address rejects a forward that keeps the hook but changes metadata.
func TestForward_HookBoundAddress_WrongMetadataRejected(t *testing.T) {
	s := newTestIGPSetup(t)
	s.useHookBoundAddressWithMetadata(t, customHookID, "0xabcdef")
	fundForwardAddr(s)

	msg := types.NewMsgForward(
		s.signer.String(), s.forwardAddr.String(), s.destDomain, s.destRecipient, s.tokenID,
		sdk.NewCoin(appconsts.BondDenom, math.NewInt(100)),
	)
	msg.CustomHookId = customHookID
	msg.CustomHookMetadata = "0xdeadbeef"

	_, err := s.msgServer.Forward(s.ctx, msg)
	require.ErrorIs(t, err, types.ErrAddressMismatch)
	require.Equal(t, math.NewInt(1000), s.bankKeeper.GetBalance(s.ctx, s.forwardAddr, appconsts.BondDenom).Amount)
}
