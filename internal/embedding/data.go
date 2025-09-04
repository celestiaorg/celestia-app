//go:build multiplexer

package embedding

import (
	"fmt"
	"runtime"
)

// NOTE: This version must be updated at the same time as the version in the
// Makefile.
const (
	v3Version = "v3.10.6"
	v4Version = "v4.1.0"
	v5Version = "v5.0.4-rc0"
)

// CelestiaAppV3 returns the compressed platform specific Celestia binary and
// the version.
func CelestiaAppV3() (version string, compressedBinary []byte, err error) {
	// Check if we actually have binary data
	if len(v3binaryCompressed) == 0 {
		return "", nil, fmt.Errorf("no binary data available for platform %s", platform())
	}

	return v3Version, v3binaryCompressed, nil
}

// CelestiaAppV4 returns the compressed platform specific Celestia binary and
// the version.
func CelestiaAppV4() (version string, compressedBinary []byte, err error) {
	if len(v4binaryCompressed) == 0 {
		return "", nil, fmt.Errorf("no binary data available for platform %s", platform())
	}

	return v4Version, v4binaryCompressed, nil
}

// CelestiaAppV5 returns the compressed platform specific Celestia binary and
// the version.
func CelestiaAppV5() (version string, compressedBinary []byte, err error) {
	if len(v5binaryCompressed) == 0 {
		return "", nil, fmt.Errorf("no binary data available for platform %s", platform())
	}

	return v5Version, v5binaryCompressed, nil
}

// platform returns a string representing the current operating system and architecture
// This is useful for identifying platform-specific binaries or configurations.
func platform() string {
	return runtime.GOOS + "_" + runtime.GOARCH
}
