package cmd

import (
	"github.com/01builders/nova/abci"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/server"
	"github.com/cosmos/cosmos-sdk/server/types"
)

// StartCommandHandler is the type that must implement nova to match Cosmos SDK start logic.
type StartCommandHandler = func(svrCtx *server.Context, clientCtx client.Context, appCreator types.AppCreator, withCmt bool, opts server.StartCmdOptions) error

// New creates a command start handler to use in the Cosmos SDK server start options.
func New(versions abci.Versions) StartCommandHandler {
	return func(
		svrCtx *server.Context,
		clientCtx client.Context,
		appCreator types.AppCreator,
		withCmt bool,
		_ server.StartCmdOptions,
	) error {
		if !withCmt {
			svrCtx.Logger.Info("App cannot be started without CometBFT when using the multiplexer.")
			return nil
		}

		return start(versions, svrCtx, clientCtx, appCreator)
	}
}
