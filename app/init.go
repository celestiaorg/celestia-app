package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	clienthelpers "cosmossdk.io/client/v2/helpers"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/celestiaorg/celestia-app/v4/pkg/appconsts"
)

// EnvPrefix is the environment variable prefix for celestia-appd.
// Environment variables that Cobra reads must be prefixed with this value.
const EnvPrefix = "CELESTIA_APP"

// Name is the name of the application.
const Name = "celestia-app"

// appDirectory is the name of the application directory. This directory is used
// to store configs, data, keyrings, etc.
const appDirectory = ".celestia-app"

// NodeHome is the home directory for the application daemon.
// This gets set as a side-effect of the init() function.
var NodeHome string

func init() {
	var err error
	clienthelpers.EnvPrefix = EnvPrefix
	NodeHome, err = getNodeHomeDirectory(appDirectory)
	fmt.Printf("NodeHome: %s\n", NodeHome)
	if err != nil {
		// The userHome is not set in Vercel's Go runtime so log a warning but don't panic.
		fmt.Printf("Warning userHome err: %s\n", err)
	}
	sdk.DefaultBondDenom = appconsts.BondDenom
}

func getNodeHomeDirectory(name string) (string, error) {
	// get the home directory from the flag
	args := os.Args
	fmt.Printf("args: %s\n", args)
	for i := 0; i < len(args); i++ {
		if args[i] == "--home" && i+1 < len(args) {
			fmt.Printf("Found home flag: %s\n", args[i+1])
			return filepath.Clean(args[i+1]), nil
		} else if strings.HasPrefix(args[i], "--home=") {
			return filepath.Clean(args[i][7:]), nil
		}
	}

	// get the home directory from the environment variable
	// to not clash with the $HOME system variable, when no prefix is set
	// we check the NODE_HOME environment variable
	homeDir, envHome := "", "HOME"
	if len(EnvPrefix) > 0 {
		homeDir = os.Getenv(EnvPrefix + "_" + envHome)
	} else {
		homeDir = os.Getenv("NODE_" + envHome)
	}
	if homeDir != "" {
		return filepath.Clean(homeDir), nil
	}

	// get user home directory
	userHomeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(userHomeDir, name), nil
}
