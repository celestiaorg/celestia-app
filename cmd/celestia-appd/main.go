// Overrides go.work too, which ignores go.mod godebug lines.

//go:debug multipathtcp=0
package main

import (
	"os"

	"github.com/celestiaorg/celestia-app/v11/app"
	"github.com/celestiaorg/celestia-app/v11/cmd/celestia-appd/cmd"
	svrcmd "github.com/cosmos/cosmos-sdk/server/cmd"
)

func main() {
	rootCmd := cmd.NewRootCmd()
	if err := svrcmd.Execute(rootCmd, app.EnvPrefix, app.NodeHome); err != nil {
		os.Exit(1)
	}
}
