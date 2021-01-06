package main

import (
	"os"

	"github.com/lazyledger/lazyledger-app/cmd/lazyledger-appd/cmd"
)

func main() {
	rootCmd, _ := cmd.NewRootCmd()
	if err := cmd.Execute(rootCmd); err != nil {
		os.Exit(1)
	}
}
