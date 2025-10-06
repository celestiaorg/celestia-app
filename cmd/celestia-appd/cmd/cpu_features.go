package cmd

import (
	"os"
	"runtime"
	"strings"

	"cosmossdk.io/log"
	"github.com/spf13/cobra"
)

const FlagTestingEnvironment = "testing-environment"

// checkCPUFeatures checks if CPU supports GFNI and SHA_NI extensions.
func checkCPUFeatures(command *cobra.Command, logger log.Logger) error {
	const (
		warning = `
CPU Performance Warning: Missing hardware acceleration features

Your CPU does not support one or more of the following hardware acceleration features:
- GFNI (Galois Field New Instructions)
- SHA_NI (SHA Extensions)

These features significantly improve cryptographic performance for blockchain operations.

Note: These features are not required for the current 32MB/6s block configuration but will become
essential when the network transitions to 128MB/6s blocks. Validators should prepare by upgrading
their hardware to ensure optimal performance during future network upgrades.

To check what features your CPU supports:
grep -o -E 'sha_ni|gfni' /proc/cpuinfo

Modern Intel CPUs (10th gen+) and AMD CPUs (Zen 4+) typically support these features.
If you are running this node in production, consider upgrading to a CPU with these features.

This node will continue to run, but may experience reduced performance for cryptographic operations.
If you need to bypass this check, use the --testing-environment flag.
`
	)

	testingEnvironment, err := command.Flags().GetBool(FlagTestingEnvironment)
	if err != nil {
		return err
	}
	if testingEnvironment {
		return nil
	}

	// Only check on Linux where /proc/cpuinfo is available
	if runtime.GOOS != "linux" {
		// Skip check silently for non-Linux OSes (e.g., macOS, Windows, BSD)
		return nil
	}

	file, err := os.ReadFile("/proc/cpuinfo")
	if err != nil {
		logger.Warn(warning)
		// TODO: enable when we want to start enforcing the new CPU features.
		//return fmt.Errorf("failed to read file '/proc/cpuinfo' %w", err)
		return nil
	}

	cpuInfo := string(file)
	hasGFNI := strings.Contains(cpuInfo, "gfni")
	hasSHANI := strings.Contains(cpuInfo, "sha_ni")

	if !hasGFNI || !hasSHANI {
		missingFeatures := []string{}
		if !hasGFNI {
			missingFeatures = append(missingFeatures, "GFNI")
		}
		if !hasSHANI {
			missingFeatures = append(missingFeatures, "SHA_NI")
		}
		logger.Warn(warning, "missing_features", strings.Join(missingFeatures, ", "))
	}

	if !hasGFNI {
		// TODO: enable when we want to start enforcing the new CPU features.
		//return fmt.Errorf("missing GFNI")
		return nil
	}

	return nil
}
