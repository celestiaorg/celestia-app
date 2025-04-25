package testdata

import (
	"fmt"
	"runtime"
)

// CelestiaApp returns the compressed platform specific Celestia binary.
func CelestiaApp() ([]byte, error) {
	// Check if we actually have binary data
	if len(binaryCompressed) == 0 {
		return nil, fmt.Errorf("no binary data available for platform %s", platform())
	}

	return binaryCompressed, nil
}

// platform returns a string representing the current operating system and architecture
// This is useful for identifying platform-specific binaries or configurations.
func platform() string {
	return runtime.GOOS + "_" + runtime.GOARCH
}
