package types

import (
	"context"

	"github.com/bcp-innovations/hyperlane-cosmos/util"
	coretypes "github.com/bcp-innovations/hyperlane-cosmos/x/core/types"
	"github.com/cosmos/cosmos-sdk/baseapp"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// MessageRouter ADR 031 request type routing
// https://github.com/cosmos/cosmos-sdk/blob/main/docs/architecture/adr-031-msg-service.md
type MessageRouter interface {
	Handler(msg sdk.Msg) baseapp.MsgServiceHandler
}

type HyperlaneKeeper interface {
	AppRouter() *util.Router[util.HyperlaneApp]
	GetMailbox(context.Context, util.HexAddress) (coretypes.Mailbox, error)
}
