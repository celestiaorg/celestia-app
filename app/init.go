package app

import (
	"os"
	"path/filepath"
)

// Name is the name of the application.
const Name = "celestia-app"

// appDirectory is the name of the application directory. This directory is used
// to store configs, data, keyrings, etc.
const appDirectory = ".celestia-app"

// nodeHome is an environment variable that sets where the app directory will be placed.
// If nodeHome isn't specified, the default user home directory will be used.
const nodeHome = "NODE_HOME"

// celestiaHome is an environment variable that sets where appDirectory will be placed.
// If celestiaHome isn't specified, the default user home directory will be used.
const celestiaHome = "CELESTIA_HOME"

// DefaultNodeHome is the default home directory for the application daemon.
// This gets set as a side-effect of the init() function.
var DefaultNodeHome string

func init() {
	nodeHome := os.Getenv(nodeHome)
	celestiaHome := os.Getenv(celestiaHome)
	userHome, _ := os.UserHomeDir() // ignore the error because the userHome is not set in Vercel's Go runtime.
	DefaultNodeHome = getDefaultNodeHome(nodeHome, celestiaHome, userHome)
}

// getDefaultNodeHome returns the location of the node home directory.
func getDefaultNodeHome(nodeHome string, celestiaHome string, userHome string) string {
	if nodeHome != "" {
		return nodeHome
	}
	if celestiaHome != "" {
		return filepath.Join(celestiaHome, appDirectory)
	}
	return filepath.Join(userHome, appDirectory)
}
