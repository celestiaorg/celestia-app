package app

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/authz"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	icahosttypes "github.com/cosmos/ibc-go/v8/modules/apps/27-interchain-accounts/host/types"
	channeltypes "github.com/cosmos/ibc-go/v8/modules/core/04-channel/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIcaAllowMessages(t *testing.T) {
	got := IcaAllowMessages()
	want := []string{
		"/ibc.applications.transfer.v1.MsgTransfer",
		"/cosmos.bank.v1beta1.MsgSend",
		"/cosmos.staking.v1beta1.MsgDelegate",
		"/cosmos.staking.v1beta1.MsgBeginRedelegate",
		"/cosmos.staking.v1beta1.MsgUndelegate",
		"/cosmos.staking.v1beta1.MsgCancelUnbondingDelegation",
		"/cosmos.distribution.v1beta1.MsgSetWithdrawAddress",
		"/cosmos.distribution.v1beta1.MsgWithdrawDelegatorReward",
		"/cosmos.distribution.v1beta1.MsgFundCommunityPool",
		"/cosmos.gov.v1.MsgVote",
		"/cosmos.feegrant.v1beta1.MsgGrantAllowance",
		"/cosmos.feegrant.v1beta1.MsgRevokeAllowance",
	}
	assert.Equal(t, want, got)
}

// TestIcaAllowMessagesExcludeFanOutMsgs guards the bound the per-block message
// limit relies on. countICAPacketMsgs counts a packet payload's entries without
// looking inside them, so a payload message that dispatches further messages
// would execute more than the block limit counts.
func TestIcaAllowMessagesExcludeFanOutMsgs(t *testing.T) {
	fanOut := map[string]string{
		sdk.MsgTypeURL(&authz.MsgExec{}):                   "dispatches the messages it wraps",
		sdk.MsgTypeURL(&icahosttypes.MsgModuleQuerySafe{}): "dispatches one query per request",
		sdk.MsgTypeURL(&channeltypes.MsgRecvPacket{}):      "dispatches the messages in its packet payload",
		sdk.MsgTypeURL(&govv1.MsgSubmitProposal{}):         "dispatches its messages once the proposal passes",
	}

	for _, allowed := range IcaAllowMessages() {
		reason, found := fanOut[allowed]
		require.False(t, found,
			"%s %s, so allowing it over ICA lets one packet execute more messages than countExecutableMsgs counts", allowed, reason)
	}
}
