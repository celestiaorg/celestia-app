//go:build !multiplexer

package cmd

import (
	"github.com/celestiaorg/celestia-app/v6/app"
	"github.com/cosmos/cosmos-sdk/server"
	"github.com/spf13/cobra"
)

// modifyRootCommand sets the default root command without adding a multiplexer.
func modifyRootCommand(rootCommand *cobra.Command) {
	server.AddCommands(rootCommand, app.NodeHome, NewAppServer, appExporter, addStartFlags)
}
