package types

import sdk "github.com/cosmos/cosmos-sdk/types"

const (
	URLMsgForward      = "/celestia.forwarding.v1.MsgForward"
	URLMsgUpdateParams = "/celestia.forwarding.v1.MsgUpdateParams"
)

var (
	_ sdk.Msg = &MsgForward{}
	_ sdk.Msg = &MsgUpdateParams{}
)
