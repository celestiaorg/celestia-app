package app

import (
	"fmt"
	"os"
	"path/filepath"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

func init() {
	initConfig()
	initHome()
}

func initHome() {
	userHomeDir := os.Getenv("CELESTIA_HOME")

	if userHomeDir == "" {
		var err error
		userHomeDir, err = os.UserHomeDir()
		if err != nil {
			panic(err)
		}
	}
	DefaultNodeHome = filepath.Join(userHomeDir, "."+Name)
}

func initConfig() {
	fmt.Printf("initConfig()\n")
	config := sdk.GetConfig()
	config.SetBech32PrefixForAccount(Bech32PrefixAccAddr, Bech32PrefixAccPub)
	config.SetBech32PrefixForValidator(Bech32PrefixValAddr, Bech32PrefixValPub)
	config.SetBech32PrefixForConsensusNode(Bech32PrefixConsAddr, Bech32PrefixConsPub)
	config.Seal()
}
