package app

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/authz"
)

// countExecutableMsgs counts messages the way a block executes them: a MsgExec
// contributes the number of messages authz dispatches from it, not one. This
// stops a tx from hiding many executable messages behind a single MsgExec to
// bypass the per-block SDK message limit. Nested MsgExec is rejected by the ante
// handler, so counting one level is enough.
func countExecutableMsgs(msgs []sdk.Msg) int {
	count := 0
	for _, msg := range msgs {
		if exec, ok := msg.(*authz.MsgExec); ok {
			count += len(exec.Msgs)
			continue
		}
		count++
	}
	return count
}
