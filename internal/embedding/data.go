//go:build multiplexer

package embedding

import (
	"fmt"
	"runtime"
)

// NOTE: This version must be updated at the same time as the version in the
// Makefile.
const v3Version = "v3.10.0-arabica"

// CelestiaAppV3 returns the compressed platform specific Celestia binary and
// the version.
func CelestiaAppV3() (version string, compressedBinary []byte, err error) {
	// Check if we actually have binary data
	if len(v3binaryCompressed) == 0 {
		return "", nil, fmt.Errorf("no binary data available for platform %s", platform())
	}

	return v3Version, v3binaryCompressed, nil
}

// platform returns a string representing the current operating system and architecture
// This is useful for identifying platform-specific binaries or configurations.
func platform() string {
	return runtime.GOOS + "_" + runtime.GOARCH
}
