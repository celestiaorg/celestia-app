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

// customHookID is an arbitrary valid Hyperlane hook id used as the "our IGP" target.
const customHookID = "0x726f757465725f706f73745f6469737061746368000000040000000000000009"

// Validates the hand-written protobuf marshal/unmarshal for the new fields so the
// hook actually survives the wire (this is what a real on-chain tx exercises).
func TestMsgForward_ProtoRoundTrip(t *testing.T) {
	m := &types.MsgForward{
		Signer:             "celestia1v8e83xs4nlflpq5vuetruxvvmtz2ll24x5hv97",
		ForwardAddr:        "celestia1mvde39xwh9c4ykzrnqfwa2trnfxu3ugczmd3t3",
		DestDomain:         714,
		DestRecipient:      "0x00000000000000000000000000000000000000000000000000000000deadbeef",
		TokenId:            "0x726f757465725f61707000000000000000000000000000010000000000000009",
		MaxIgpFee:          sdk.NewCoin(appconsts.BondDenom, math.NewInt(10000)),
		CustomHookId:       customHookID,
		CustomHookMetadata: "0xabcdef",
	}
	bz, err := m.Marshal()
	require.NoError(t, err)
	require.Equal(t, m.Size(), len(bz), "Size() must match marshaled length")

	var out types.MsgForward
	require.NoError(t, out.Unmarshal(bz))
	require.Equal(t, m.CustomHookId, out.CustomHookId)
	require.Equal(t, m.CustomHookMetadata, out.CustomHookMetadata)
	require.Equal(t, m.MaxIgpFee, out.MaxIgpFee)
	require.Equal(t, m.TokenId, out.TokenId)
	require.Equal(t, m.DestDomain, out.DestDomain)

	// Backward compatible: a message without the new fields still round-trips, empty.
	old := &types.MsgForward{Signer: "a", DestDomain: 1, MaxIgpFee: sdk.NewCoin(appconsts.BondDenom, math.NewInt(1))}
	obz, err := old.Marshal()
	require.NoError(t, err)
	var oout types.MsgForward
	require.NoError(t, oout.Unmarshal(obz))
	require.Empty(t, oout.CustomHookId)
	require.Empty(t, oout.CustomHookMetadata)
}

// fund + wire a forward that will succeed, consuming the forwarded amount + part of the fee.
func setupSuccessfulForward(s *testIGPSetup) {
	s.bankKeeper.Balances[s.forwardAddr.String()] = sdk.NewCoins(sdk.NewCoin(appconsts.BondDenom, math.NewInt(1000)))
	s.bankKeeper.Balances[s.signer.String()] = sdk.NewCoins(sdk.NewCoin(appconsts.BondDenom, math.NewInt(200)))
	s.hyperlaneKeeper.QuotedFee = sdk.NewCoins(sdk.NewCoin(appconsts.BondDenom, math.NewInt(100)))
	messageId, _ := util.DecodeHexAddress("0x0000000000000000000000000000000000000000000000000000000000001234")
	s.warpKeeper.TransferMessageId = messageId
	forwarded := math.NewInt(1000)
	igpConsumed := math.NewInt(80)
	s.warpKeeper.OnTransfer = func(sender string, maxFee sdk.Coin) {
		senderAddr, _ := sdk.AccAddressFromBech32(sender)
		cur := s.bankKeeper.Balances[senderAddr.String()]
		s.bankKeeper.Balances[senderAddr.String()] = cur.Sub(sdk.NewCoin(maxFee.Denom, forwarded.Add(igpConsumed)))
	}
}

// With custom_hook_id set, both the fee quote and the warp transfer must use that
// exact hook — this is what routes the payment to our IGP (and thus our relayer).
func TestForward_CustomHookId_RoutesToChosenHook(t *testing.T) {
	s := newTestIGPSetup(t)
	setupSuccessfulForward(s)

	msg := types.NewMsgForward(
		s.signer.String(), s.forwardAddr.String(), s.destDomain, s.destRecipient, s.tokenID,
		sdk.NewCoin(appconsts.BondDenom, math.NewInt(100)),
	)
	msg.CustomHookId = customHookID

	resp, err := s.msgServer.Forward(s.ctx, msg)
	require.NoError(t, err)
	require.NotNil(t, resp)

	want, err := util.DecodeHexAddress(customHookID)
	require.NoError(t, err)

	// Fee was quoted against our hook (not the default zero hook).
	require.Equal(t, want, s.hyperlaneKeeper.CapturedHook, "fee must be quoted against the custom hook")
	// The warp transfer dispatched through our hook.
	require.NotNil(t, s.warpKeeper.CapturedHookId, "custom hook id must be passed to the warp transfer")
	require.Equal(t, want, *s.warpKeeper.CapturedHookId, "warp transfer must use the custom hook")
}

// Without custom_hook_id, behavior is unchanged: default (zero) hook, nil to warp.
func TestForward_NoCustomHook_UsesDefault(t *testing.T) {
	s := newTestIGPSetup(t)
	setupSuccessfulForward(s)

	msg := types.NewMsgForward(
		s.signer.String(), s.forwardAddr.String(), s.destDomain, s.destRecipient, s.tokenID,
		sdk.NewCoin(appconsts.BondDenom, math.NewInt(100)),
	)
	// CustomHookId intentionally left empty.

	resp, err := s.msgServer.Forward(s.ctx, msg)
	require.NoError(t, err)
	require.NotNil(t, resp)

	require.Equal(t, util.NewZeroAddress(), s.hyperlaneKeeper.CapturedHook, "empty custom hook must quote against the default (zero) hook")
	require.Nil(t, s.warpKeeper.CapturedHookId, "empty custom hook must pass nil to the warp transfer (mailbox default)")
}

// An invalid custom_hook_id is rejected before any funds move.
func TestForward_InvalidCustomHookId_Rejected(t *testing.T) {
	s := newTestIGPSetup(t)
	setupSuccessfulForward(s)
	signerBefore := s.bankKeeper.GetBalance(s.ctx, s.signer, appconsts.BondDenom).Amount

	msg := types.NewMsgForward(
		s.signer.String(), s.forwardAddr.String(), s.destDomain, s.destRecipient, s.tokenID,
		sdk.NewCoin(appconsts.BondDenom, math.NewInt(100)),
	)
	msg.CustomHookId = "not-a-valid-hex-hook"

	resp, err := s.msgServer.Forward(s.ctx, msg)
	require.Error(t, err)
	require.Nil(t, resp)
	require.ErrorContains(t, err, "custom_hook_id")
	// No funds moved and no warp transfer attempted (rejected during parsing).
	require.Equal(t, signerBefore, s.bankKeeper.GetBalance(s.ctx, s.signer, appconsts.BondDenom).Amount)
	require.Nil(t, s.warpKeeper.CapturedHookId, "no warp transfer should have been attempted")
}
