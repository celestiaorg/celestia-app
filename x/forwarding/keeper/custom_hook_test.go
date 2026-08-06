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

// nonDefaultHookID is a valid non-default Hyperlane hook ID.
const nonDefaultHookID = "0x726f757465725f706f73745f6469737061746368000000040000000000000009"

// freeHookID is a valid hook ID that charges no fee.
const freeHookID = "0x726f757465725f706f73745f6469737061746368000000000000000000000001"

// TestMsgForwardPreservesHookFieldsDuringProtobufRoundTrip verifies that hook
// fields are not dropped during serialization.
func TestMsgForwardPreservesHookFieldsDuringProtobufRoundTrip(t *testing.T) {
	m := &types.MsgForward{
		Signer:             "celestia1v8e83xs4nlflpq5vuetruxvvmtz2ll24x5hv97",
		ForwardAddr:        "celestia1mvde39xwh9c4ykzrnqfwa2trnfxu3ugczmd3t3",
		DestDomain:         714,
		DestRecipient:      "0x00000000000000000000000000000000000000000000000000000000deadbeef",
		TokenId:            "0x726f757465725f61707000000000000000000000000000010000000000000009",
		MaxIgpFee:          sdk.NewCoin(appconsts.BondDenom, math.NewInt(10000)),
		CustomHookId:       nonDefaultHookID,
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

	// Messages without hook fields still round-trip.
	old := &types.MsgForward{Signer: "a", DestDomain: 1, MaxIgpFee: sdk.NewCoin(appconsts.BondDenom, math.NewInt(1))}
	obz, err := old.Marshal()
	require.NoError(t, err)
	var oout types.MsgForward
	require.NoError(t, oout.Unmarshal(obz))
	require.Empty(t, oout.CustomHookId)
	require.Empty(t, oout.CustomHookMetadata)
}

// setupSuccessfulForward configures a valid, funded forward.
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

// TestForwardRejectsCallerSuppliedHookFieldsBeforeMovingFunds verifies that any
// caller-supplied hook ID or metadata rejects the forward without moving funds.
func TestForwardRejectsCallerSuppliedHookFieldsBeforeMovingFunds(t *testing.T) {
	testCases := []struct {
		name         string
		hookID       string
		hookMetadata string
		wantErr      error
	}{
		{
			name:         "rejects non-default hook ID and metadata",
			hookID:       nonDefaultHookID,
			hookMetadata: "0xabcdef",
			wantErr:      types.ErrCustomHookNotAllowed,
		},
		{
			// A free hook could dispatch without funding delivery.
			name:    "rejects free hook ID",
			hookID:  freeHookID,
			wantErr: types.ErrCustomHookNotAllowed,
		},
		{
			// Metadata can change a hook's fee even without a hook ID.
			name:         "rejects metadata when hook ID is empty",
			hookMetadata: "0xabcdef",
			wantErr:      types.ErrCustomHookNotAllowed,
		},
		{
			name:    "rejects zero-address hook ID",
			hookID:  util.NewZeroAddress().String(),
			wantErr: types.ErrCustomHookNotAllowed,
		},
		{
			// Hook fields are rejected before parsing.
			name:    "rejects malformed hook ID",
			hookID:  "not-a-valid-hex-hook",
			wantErr: types.ErrCustomHookNotAllowed,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestIGPSetup(t)
			setupSuccessfulForward(s)

			// Record whether the warp transfer is reached.
			transferred := false
			s.warpKeeper.OnTransfer = func(string, sdk.Coin) { transferred = true }
			principalBefore := s.bankKeeper.GetBalance(s.ctx, s.forwardAddr, appconsts.BondDenom).Amount
			signerBefore := s.bankKeeper.GetBalance(s.ctx, s.signer, appconsts.BondDenom).Amount

			msg := types.NewMsgForward(
				s.signer.String(), s.forwardAddr.String(), s.destDomain, s.destRecipient, s.tokenID,
				sdk.NewCoin(appconsts.BondDenom, math.NewInt(100)),
			)
			msg.CustomHookId = tc.hookID
			msg.CustomHookMetadata = tc.hookMetadata

			resp, err := s.msgServer.Forward(s.ctx, msg)
			require.Nil(t, resp)
			require.ErrorIs(t, err, tc.wantErr)

			require.False(t, transferred, "no warp transfer may be attempted")
			require.Equal(t, principalBefore, s.bankKeeper.GetBalance(s.ctx, s.forwardAddr, appconsts.BondDenom).Amount,
				"the deposit must stay at the forwarding address")
			require.Equal(t, signerBefore, s.bankKeeper.GetBalance(s.ctx, s.signer, appconsts.BondDenom).Amount,
				"no IGP fee may be collected from the relayer")
		})
	}
}

// TestForwardWithEmptyHookFieldsUsesMailboxDefaultHook verifies that forwarding
// uses the mailbox default hook when the caller leaves both hook fields empty.
func TestForwardWithEmptyHookFieldsUsesMailboxDefaultHook(t *testing.T) {
	s := newTestIGPSetup(t)
	setupSuccessfulForward(s)

	msg := types.NewMsgForward(
		s.signer.String(), s.forwardAddr.String(), s.destDomain, s.destRecipient, s.tokenID,
		sdk.NewCoin(appconsts.BondDenom, math.NewInt(100)),
	)

	resp, err := s.msgServer.Forward(s.ctx, msg)
	require.NoError(t, err)
	require.NotNil(t, resp)

	require.Equal(t, util.NewZeroAddress(), s.hyperlaneKeeper.CapturedHook, "must quote against the mailbox default hook")
	require.Empty(t, s.hyperlaneKeeper.CapturedQuoteMeta, "no caller metadata may reach the hook")
	require.Nil(t, s.warpKeeper.CapturedHookId, "must pass nil to the warp transfer")
	require.Empty(t, s.warpKeeper.CapturedHookMeta, "must pass no hook metadata to the warp transfer")
}
