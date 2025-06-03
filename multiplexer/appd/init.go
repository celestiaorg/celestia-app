package appd

import (
	"fmt"

	clienthelpers "cosmossdk.io/client/v2/helpers"
)

// envPrefix is the environment variable prefix for celestia-appd.
// Environment variables that Cobra reads must be prefixed with this value.
const envPrefix = "CELESTIA_APP"

// appDirectory is the name of the application directory. This directory is used
// to store configs, data, keyrings, and binaries.
const appDirectory = ".celestia-app"

// nodeHome is the home directory for the application daemon. By default this is
// $HOME/.celestia-app but it can be overridden by the user via --home flag.
//
// This gets set as a side-effect of the init() function.
var nodeHome string

func init() {
	clienthelpers.EnvPrefix = envPrefix

	var err error
	nodeHome, err = clienthelpers.GetNodeHomeDirectory(appDirectory)
	if err != nil {
		// The userHome is not set in Vercel's Go runtime so log a warning but don't panic.
		fmt.Printf("Warning userHome err: %s\n", err)
	}
}
