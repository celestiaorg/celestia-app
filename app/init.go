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

// celestiaHome is an environment variable that sets where appDirectory will be placed.
// If celestiaHome isn't specified, the default user home directory will be used.
const celestiaHome = "CELESTIA_HOME"

// DefaultNodeHome is the home directory for the app directory. In other words,
// this is the path to the directory that will contain .celestia-app. This gets
// set as a side-effect of the init() function.
var DefaultNodeHome string

func init() {
	celestiaHome := os.Getenv(celestiaHome)
	userHome, err := os.UserHomeDir()
	if err != nil {
		// The userHome is not set in Vercel's Go runtime so log a warning but don't panic.
		fmt.Printf("Warning userHome err: %s\n", err)
	}
	DefaultNodeHome = getDefaultNodeHome(celestiaHome, userHome)
}

// getDefaultNodeHome returns the location of the default home app directory.
func getDefaultNodeHome(celestiaHome string, userHome string) string {
	if celestiaHome != "" {
		return filepath.Join(celestiaHome, appDirectory)
	}
	return filepath.Join(userHome, appDirectory)
}
