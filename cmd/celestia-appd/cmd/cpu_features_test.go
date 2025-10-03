package cmd

import (
	"runtime"
	"strings"
	"testing"

	"cosmossdk.io/log"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
)

func TestCheckCPUFeatures(t *testing.T) {
	logger := log.NewNopLogger()

	tests := []struct {
		name           string
		testingEnvFlag bool
		expectError    bool
	}{
		{
			name:           "testing environment flag bypasses check",
			testingEnvFlag: true,
			expectError:    false,
		},
		{
			name:           "normal check on current system",
			testingEnvFlag: false,
			expectError:    false, // Should not error, only warn
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := &cobra.Command{}
			cmd.Flags().Bool(FlagTestingEnvironment, tt.testingEnvFlag, "test flag")

			err := checkCPUFeatures(cmd, logger)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestCPUFeatureDetectionLogic(t *testing.T) {
	tests := []struct {
		name          string
		cpuinfo       string
		expectGFNI    bool
		expectSHANI   bool
	}{
		{
			name:        "both features present",
			cpuinfo:     "flags : fpu vme de pse tsc gfni sha_ni aes",
			expectGFNI:  true,
			expectSHANI: true,
		},
		{
			name:        "missing gfni",
			cpuinfo:     "flags : fpu vme de pse tsc sha_ni aes",
			expectGFNI:  false,
			expectSHANI: true,
		},
		{
			name:        "missing sha_ni",
			cpuinfo:     "flags : fpu vme de pse tsc gfni aes",
			expectGFNI:  true,
			expectSHANI: false,
		},
		{
			name:        "missing both features",
			cpuinfo:     "flags : fpu vme de pse tsc aes",
			expectGFNI:  false,
			expectSHANI: false,
		},
		{
			name:        "empty cpuinfo",
			cpuinfo:     "",
			expectGFNI:  false,
			expectSHANI: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hasGFNI := strings.Contains(tt.cpuinfo, "gfni")
			hasSHANI := strings.Contains(tt.cpuinfo, "sha_ni")

			assert.Equal(t, tt.expectGFNI, hasGFNI, "GFNI detection mismatch")
			assert.Equal(t, tt.expectSHANI, hasSHANI, "SHA_NI detection mismatch")
		})
	}
}

func TestCheckCPUFeaturesNonLinux(t *testing.T) {
	if runtime.GOOS == "linux" {
		t.Skip("This test is for non-Linux systems only")
	}

	logger := log.NewNopLogger()
	cmd := &cobra.Command{}
	cmd.Flags().Bool(FlagTestingEnvironment, false, "test flag")

	// On non-Linux systems, the check should always pass silently
	err := checkCPUFeatures(cmd, logger)
	assert.NoError(t, err)
}