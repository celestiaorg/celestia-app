package types

import sdk "github.com/cosmos/cosmos-sdk/types"

var (
	_ sdk.Msg = (*MsgPayForBlobs)(nil)
	_ sdk.Msg = (*MsgUpdateBlobParams)(nil)
)

// NewMsgUpdateBlobParams creates a new MsgUpdateBlobParams instance.
func NewMsgUpdateBlobParams(authority string, params Params) *MsgUpdateBlobParams {
	return &MsgUpdateBlobParams{
		Authority: authority,
		Params:    params,
	}
}
