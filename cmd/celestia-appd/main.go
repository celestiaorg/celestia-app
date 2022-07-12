package main

import (
	"os"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/cmd/celestia-appd/cmd"

	svrcmd "github.com/cosmos/cosmos-sdk/server/cmd"
)

func main() {
	rootCmd := cmd.NewRootCmd()
	if err := svrcmd.Execute(rootCmd, cmd.EnvPrefix, app.DefaultNodeHome); err != nil {
		os.Exit(1)
	}
}
