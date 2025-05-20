//go:build multiplexer

package embedding

import (
	"fmt"
	"os"
	"runtime"
	"strings"
)

const (
	// EmbedVersionFile is the file that contains the v3.x.x version of the
	// Celestia binary.
	EmbedVersionFile = ".embed_version"
)

// CelestiaAppV3 returns the compressed platform specific Celestia binary.
func CelestiaAppV3() (binary []byte, version string, err error) {
	// Check if we actually have binary data
	if len(v3binaryCompressed) == 0 {
		return nil, "", fmt.Errorf("no binary data available for platform %s", platform())
	}
	version, err = getVersion()
	if err != nil {
		return nil, "", fmt.Errorf("failed to get version: %w", err)
	}

	return v3binaryCompressed, version, nil
}

// platform returns a string representing the current operating system and architecture
// This is useful for identifying platform-specific binaries or configurations.
func platform() string {
	return runtime.GOOS + "_" + runtime.GOARCH
}

// getVersion returns the v3.x.x version (e.g. v3.10.0-arabica) that was written
// to the .embed_version file.
func getVersion() (string, error) {
	contents, err := os.ReadFile(EmbedVersionFile)
	if err != nil {
		return "", fmt.Errorf("failed to read .embed_version file: %w", err)
	}
	version := strings.TrimSuffix(string(contents), "\n")
	return version, nil
}
