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

// bindAddress binds the setup's address to (hook, metadata); both empty leaves it default.
func bindAddress(t *testing.T, s *testIGPSetup, hook, metadata string) {
	t.Helper()
	if hook != "" || metadata != "" {
		s.useHookBoundAddressWithMetadata(t, hook, metadata)
	}
}

// fundForwardAddr puts a deposit at the forwarding address and funds the signer's IGP fee.
func fundForwardAddr(s *testIGPSetup) {
	s.bankKeeper.Balances[s.forwardAddr.String()] = sdk.NewCoins(sdk.NewCoin(appconsts.BondDenom, math.NewInt(1000)))
	s.bankKeeper.Balances[s.signer.String()] = sdk.NewCoins(sdk.NewCoin(appconsts.BondDenom, math.NewInt(500)))
	s.hyperlaneKeeper.QuotedFee = sdk.NewCoins(sdk.NewCoin(appconsts.BondDenom, math.NewInt(100)))
}

func forwardMsg(s *testIGPSetup, hook, metadata string) *types.MsgForward {
	msg := types.NewMsgForward(
		s.signer.String(), s.forwardAddr.String(), s.destDomain, s.destRecipient, s.tokenID,
		sdk.NewCoin(appconsts.BondDenom, math.NewInt(100)),
	)
	msg.CustomHookId = hook
	msg.CustomHookMetadata = metadata
	return msg
}

// A deposit can only be forwarded with the exact (hook, metadata) pair its address commits
// to. This is what stops a caller routing someone else's deposit through a hook of their
// choosing, e.g. a free one that funds no delivery.
//
// These cases cover the three ways Forward can pick a derivation: either field supplied
// selects the bound scheme, neither selects the default one. That a *different* pair lands on
// a different address is the derivation's contract, covered by
// TestDeriveForwardingAddressWithHookIsDistinct rather than re-tested here.
func TestForward_BindingMismatchRejected(t *testing.T) {
	const meta = "0xabcdef"

	testCases := []struct {
		name         string
		bindHook     string
		bindMetadata string
		sendHook     string
		sendMetadata string
	}{
		{"hook supplied for a default address", "", "", customHookID, ""},
		{"metadata supplied for a default address", "", "", "", meta},
		{"binding omitted for a bound address", customHookID, meta, "", ""},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestIGPSetup(t)
			bindAddress(t, s, tc.bindHook, tc.bindMetadata)
			fundForwardAddr(s)

			_, err := s.msgServer.Forward(s.ctx, forwardMsg(s, tc.sendHook, tc.sendMetadata))
			require.ErrorIs(t, err, types.ErrAddressMismatch)

			// Rejected before anything moved.
			require.Equal(t, math.NewInt(1000), s.bankKeeper.GetBalance(s.ctx, s.forwardAddr, appconsts.BondDenom).Amount)
			require.Nil(t, s.warpKeeper.CapturedHookId)
		})
	}
}

// Matching the binding forwards and dispatches through the committed hook. Metadata-only
// means the default hook, so it dispatches nil. hook+metadata is covered by
// TestForward_CustomHookId_RoutesToChosenHook.
func TestForward_BindingMatchAccepted(t *testing.T) {
	testCases := []struct {
		name             string
		hook             string
		metadata         string
		wantDispatchHook string // "" => mailbox default (nil to warp)
	}{
		{"hook only", customHookID, "", customHookID},
		{"metadata only", "", "0xabcdef", ""},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestIGPSetup(t)
			bindAddress(t, s, tc.hook, tc.metadata)
			setupSuccessfulForward(s)

			resp, err := s.msgServer.Forward(s.ctx, forwardMsg(s, tc.hook, tc.metadata))
			require.NoError(t, err)
			require.NotNil(t, resp)

			if tc.wantDispatchHook == "" {
				require.Nil(t, s.warpKeeper.CapturedHookId, "must dispatch through the mailbox default hook")
				return
			}
			want, err := util.DecodeHexAddress(tc.wantDispatchHook)
			require.NoError(t, err)
			require.Equal(t, want, s.hyperlaneKeeper.CapturedHook, "fee must be quoted against the bound hook")
			require.NotNil(t, s.warpKeeper.CapturedHookId)
			require.Equal(t, want, *s.warpKeeper.CapturedHookId, "dispatch must use the bound hook")
		})
	}
}
