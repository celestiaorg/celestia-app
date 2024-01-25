package app

import (
	"os"
	"path/filepath"
)

// Name is the name of the application.
const Name = "celestia-app"

// CelestiaHome is the environment variable for the home directory of the application daemon.
// If not set, the default user home directory will be used.
const CelestiaHome = "CELESTIA_HOME"

// DefaultNodeHome is the default home directory for the application daemon.
// This gets set as a side-effect of the init() function.
var DefaultNodeHome string

func init() {
	userHome, err := os.UserHomeDir()
	if err != nil {
		panic(err)
	}
	celestiaHome := os.Getenv(CelestiaHome)
	DefaultNodeHome = getDefaultNodeHome(userHome, celestiaHome)
}

// getDefaultNodeHome computes the default node home directory based on the provided userHome and celestiaHome.
// If celestiaHome is provided, it takes precedence and constructs the path by appending the application directory.
// Otherwise, it falls back to using the userHome with the application directory appended.
func getDefaultNodeHome(userHome string, celestiaHome string) string {
	appDir := "." + Name
	if celestiaHome != "" {
		return filepath.Join(celestiaHome, appDir)
	}
	return filepath.Join(userHome, appDir)
}
