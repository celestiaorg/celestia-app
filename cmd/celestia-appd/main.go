package main

import (
	"os"

	"github.com/celestiaorg/celestia-app/v5/app"
	"github.com/celestiaorg/celestia-app/v5/cmd/celestia-appd/cmd"
	svrcmd "github.com/cosmos/cosmos-sdk/server/cmd"
)

func main() {
	rootCmd := cmd.NewRootCmd()
	if err := svrcmd.Execute(rootCmd, app.EnvPrefix, app.NodeHome); err != nil {
		os.Exit(1)
	}
}
