package main

import (
	"os"

	"github.com/celestiaorg/celestia-app/app"
	svrcmd "github.com/cosmos/cosmos-sdk/server/cmd"
)

const envPrefix = "CELESTIA"

func main() {
	rootCmd := NewRootCmd()
	if err := svrcmd.Execute(rootCmd, envPrefix, app.DefaultNodeHome); err != nil {
		os.Exit(1)
	}
}
