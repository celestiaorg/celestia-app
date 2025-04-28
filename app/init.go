package app

import (
	"fmt"
	"os"
	"path/filepath"
)

// Name is the name of the application.
const Name = "celestia-app"

// appDirectory is the name of the application directory. This directory is used
// to store configs, data, keyrings, etc.
const appDirectory = ".celestia-app"

// celestiaHome is an environment variable that sets where data will be placed.
// If celestiaHome isn't specified, the default user home directory will be used.
const celestiaHome = "CELESTIA_APP_HOME"

// celestiaHomeOld is an environment variable that sets where appDirectory will be placed.
// If celestiaHomeOld isn't specified, the default user home directory will be used.
// Deprecated.
const celestiaHomeOld = "CELESTIA_HOME"

// DefaultNodeHome is the default home directory for the application daemon.
// This gets set as a side-effect of the init() function.
var DefaultNodeHome string

func init() {
	userHome, err := os.UserHomeDir()
	if err != nil {
		panic(err)
	}
	celestiaHome := os.Getenv(celestiaHome)
	celestiaHomeOld := os.Getenv(celestiaHomeOld)
	DefaultNodeHome = getDefaultNodeHome(userHome, celestiaHome, celestiaHomeOld)
}

// getDefaultNodeHome computes the default node home directory based on the
// provided userHome and celestiaHome. If celestiaHome is provided, it takes
// precedence and constructs the path.
// Otherwise, it falls back to using the userHome with the application directory
// appended.
func getDefaultNodeHome(userHome string, celestiaHome string, celestiaHomeOld string) string {
	if celestiaHome != "" {
		return celestiaHome
	}
	if celestiaHomeOld != "" {
		fmt.Print("warning CELESTIA_HOME is deprecated and will be removed in the next major release. Use CELESTIA_APP_HOME instead.\n")
		return filepath.Join(celestiaHomeOld, appDirectory)
	}
	return filepath.Join(userHome, appDirectory)
}
