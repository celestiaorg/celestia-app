package cmd

import (
	"fmt"

	"github.com/celestiaorg/celestia-app/v10/multiplexer/abci"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/server"
	"github.com/cosmos/cosmos-sdk/server/types"
	"github.com/spf13/cobra"
)

// StartCommandHandler is the type that must implement the multiplexer to match Cosmos SDK start logic.
type StartCommandHandler = func(svrCtx *server.Context, clientCtx client.Context, appCreator types.AppCreator, withCmt bool, opts server.StartCmdOptions) error

// New creates a command start handler to use in the Cosmos SDK server start options.
func New(versions abci.Versions, rootCommand *cobra.Command) StartCommandHandler {
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

		startCommand, _, err := rootCommand.Find([]string{"start"})
		if err != nil {
			return fmt.Errorf("find start command: %w", err)
		}
		return start(versions, svrCtx, clientCtx, appCreator, startCommand.Flags())
	}
}
