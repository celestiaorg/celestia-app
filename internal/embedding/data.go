//go:build multiplexer

package embedding

import (
	"fmt"
	"runtime"
)

// CelestiaAppV3 returns the compressed platform specific Celestia binary.
func CelestiaAppV3() ([]byte, error) {
	// Check if we actually have binary data
	if len(v3binaryCompressed) == 0 {
		return nil, fmt.Errorf("no binary data available for platform %s", platform())
	}

	return v3binaryCompressed, nil
}

// platform returns a string representing the current operating system and architecture
// This is useful for identifying platform-specific binaries or configurations.
func platform() string {
	return runtime.GOOS + "_" + runtime.GOARCH
}
