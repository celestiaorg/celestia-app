package cmd

import (
	"fmt"
	"os"
	"runtime"
	"strings"

	"cosmossdk.io/log"
	"github.com/spf13/cobra"
)

const FlagTestingEnvironment = "testing-environment"

// checkCPUFeatures checks if GFNI and SHA_NI CPU features are available.
// It should be run before RunE of the StartCmd.
func checkCPUFeatures(command *cobra.Command, logger log.Logger) error {
	const (
		warning = `
GFNI and/or SHA_NI CPU features are not available on this system.
These features can significantly improve cryptographic performance for validators.

GFNI (Galois Field New Instructions) and SHA_NI (SHA New Instructions) are CPU extensions
that accelerate certain cryptographic operations used in consensus and validation.

To check if your CPU supports these features:
grep -E 'gfni|sha_ni' /proc/cpuinfo

Consider upgrading to a CPU that supports these features for optimal performance.
If you are running in a testing environment, you can bypass this check using the --testing-environment flag.
`
	)

	testingEnv, err := command.Flags().GetBool(FlagTestingEnvironment)
	if err != nil {
		return err
	}
	if testingEnv {
		return nil
	}

	// Only check CPU features on Linux where /proc is available
	if runtime.GOOS != "linux" {
		// Skip check silently for non-Linux OSes (e.g., macOS, Windows, BSD)
		return nil
	}

	file, err := os.ReadFile("/proc/cpuinfo")
	if err != nil {
		logger.Warn(warning)
		logger.Warn(fmt.Sprintf("failed to read /proc/cpuinfo: %v", err))
		return nil // Don't block startup, just warn
	}

	content := string(file)
	hasGFNI := strings.Contains(content, "gfni")
	hasSHANI := strings.Contains(content, "sha_ni")

	if !hasGFNI || !hasSHANI {
		logger.Warn(warning)
		if !hasGFNI {
			logger.Warn("GFNI CPU feature not found")
		}
		if !hasSHANI {
			logger.Warn("SHA_NI CPU feature not found")
		}
	}

	return nil
}