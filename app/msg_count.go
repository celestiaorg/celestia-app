package app

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/authz"
	icahosttypes "github.com/cosmos/ibc-go/v8/modules/apps/27-interchain-accounts/host/types"
)

// countExecutableMsgs counts messages the way a block executes them: a MsgExec
// contributes the messages authz dispatches from it and a MsgModuleQuerySafe
// contributes the queries it dispatches, not one each. This stops a tx from
// hiding many units of execution behind a single message to bypass the
// per-block SDK message limit. Nested MsgExec is rejected by the ante handler,
// so flattening one level is enough.
func countExecutableMsgs(msgs []sdk.Msg) int {
	count := 0
	for _, msg := range msgs {
		exec, ok := msg.(*authz.MsgExec)
		if !ok {
			count += msgWeight(msg)
			continue
		}
		nested, err := exec.GetMessages()
		if err != nil {
			// The ante handler rejects a tx with undecodable inner messages, so
			// count the wrappers rather than treating the MsgExec as free.
			count += len(exec.Msgs)
			continue
		}
		for _, inner := range nested {
			count += msgWeight(inner)
		}
	}
	return count
}

// msgWeight returns the number of executable units in a single message. It never
// returns less than one: a message with an empty fan-out still costs a full ante
// pass, and per-message ValidateBasic does not run in the ante chain that
// ProcessProposal uses.
func msgWeight(msg sdk.Msg) int {
	if querySafe, ok := msg.(*icahosttypes.MsgModuleQuerySafe); ok {
		return max(1, len(querySafe.Requests))
	}
	return 1
}
