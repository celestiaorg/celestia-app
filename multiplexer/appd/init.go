package appd

import (
	"fmt"

	clienthelpers "cosmossdk.io/client/v2/helpers"
)

// EnvPrefix is the environment variable prefix for celestia-appd.
// Environment variables that Cobra reads must be prefixed with this value.
const EnvPrefix = "CELESTIA_APP"

// appDirectory is the name of the application directory. This directory is used
// to store configs, data, keyrings, and binaries.
const appDirectory = ".celestia-app"

// NodeHome is the home directory for the application daemon.
// This gets set as a side-effect of the init() function.
var NodeHome string

func init() {
	clienthelpers.EnvPrefix = EnvPrefix

	var err error
	NodeHome, err = clienthelpers.GetNodeHomeDirectory(appDirectory)
	if err != nil {
		// The userHome is not set in Vercel's Go runtime so log a warning but don't panic.
		fmt.Printf("Warning userHome err: %s\n", err)
	}
}
