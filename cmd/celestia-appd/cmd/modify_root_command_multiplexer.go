//go:build multiplexer

package cmd

import (
	"github.com/01builders/nova"
	"github.com/celestiaorg/celestia-app/v4/app"
	"github.com/cosmos/cosmos-sdk/server"
	"github.com/spf13/cobra"
)

// modifyRootCommand enhances the root command with the pass through and multiplexer.
func modifyRootCommand(rootCommand *cobra.Command) {
	versions := Versions()
	passthroughCmd, err := nova.NewPassthroughCmd(versions)
	_ = err // TODO: handle the error in this case.
	rootCommand.AddCommand(passthroughCmd)
	// Add the following commands to the rootCommand: start, tendermint, export, version, and rollback.
	server.AddCommandsWithStartCmdOptions(rootCommand, app.DefaultNodeHome, NewAppServer, appExporter, server.StartCmdOptions{
		AddFlags:            addStartFlags,
		StartCommandHandler: nova.New(versions), // multiplexer
	})
}
