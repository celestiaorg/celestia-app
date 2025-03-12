package app

import (
	clienthelpers "cosmossdk.io/client/v2/helpers"
)

// EnvPrefix is the environment variable prefix for celestia-appd.
// Environment variables that Cobra reads must be prefixed with this value.
const EnvPrefix = "CELESTIA"

// Name is the name of the application.
const Name = "celestia-app"

// appDirectory is the name of the application directory. This directory is used
// to store configs, data, keyrings, etc.
const appDirectory = ".celestia-app"

// DefaultNodeHome is the default home directory for the application daemon.
// This gets set as a side-effect of the init() function.
var DefaultNodeHome string

func init() {
	var err error
	clienthelpers.EnvPrefix = EnvPrefix
	DefaultNodeHome, err = clienthelpers.GetNodeHomeDirectory(appDirectory)
	if err != nil {
		panic(err)
	}
}
