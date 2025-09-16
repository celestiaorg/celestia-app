package app

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"cosmossdk.io/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLoggerIntegration demonstrates that the logger wrapper correctly handles
// the exact error messages from the issue description
func TestLoggerIntegration(t *testing.T) {
	testCases := []struct {
		name        string
		errorMsg    string
		expectDebug bool
		description string
	}{
		{
			name:        "WriteFlat gas error",
			errorMsg:    "out of gas in location: WriteFlat; gasWanted: 300000, gasUsed: 301934: out of gas",
			expectDebug: true,
			description: "First error from issue #5765",
		},
		{
			name:        "WritePerByte gas error", 
			errorMsg:    "out of gas in location: WritePerByte; gasWanted: 300000, gasUsed: 300033: out of gas",
			expectDebug: true,
			description: "Second error from issue #5765",
		},
		{
			name:        "WriteFlat gas error variant",
			errorMsg:    "out of gas in location: WriteFlat; gasWanted: 300000, gasUsed: 300121: out of gas",
			expectDebug: true,
			description: "Third error from issue #5765",
		},
		{
			name:        "Non-gas panic error",
			errorMsg:    "invalid transaction signature",
			expectDebug: false,
			description: "Non-gas errors should remain at INFO level",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			logger := log.NewLogger(&buf, log.ColorOption(false))
			
			// Wrap the logger like the application does
			wrappedLogger := NewTxErrorLoggerWrapper(logger)
			
			// Simulate the exact log call from cosmos-sdk BaseApp
			wrappedLogger.Info("panic recovered in runTx", "err", tc.errorMsg)
			
			output := buf.String()
			
			if tc.expectDebug {
				assert.Contains(t, output, "DBG", "Gas error should be logged at DEBUG level")
				assert.NotContains(t, output, "INF", "Gas error should not be at INFO level")
				assert.Contains(t, output, tc.errorMsg, "Error message should be preserved")
			} else {
				assert.Contains(t, output, "INF", "Non-gas error should remain at INFO level")
				assert.NotContains(t, output, "DBG", "Non-gas error should not be at DEBUG level")
				assert.Contains(t, output, tc.errorMsg, "Error message should be preserved")
			}
			
			// Verify the log contains the expected parts
			assert.Contains(t, output, "panic recovered in runTx", "Message should be preserved")
			assert.Contains(t, output, "err=", "Error key should be present")
		})
	}
}

// TestLoggerWithModuleContext verifies that the logger wrapper preserves
// the module context that would be present in actual cosmos-sdk logs
func TestLoggerWithModuleContext(t *testing.T) {
	var buf bytes.Buffer
	logger := log.NewLogger(&buf, log.ColorOption(false))
	
	// Simulate the logger with module context like cosmos-sdk does
	wrappedLogger := NewTxErrorLoggerWrapper(logger).With("module", "server")
	
	// Log a gas error
	wrappedLogger.Info("panic recovered in runTx", "err", "out of gas in location: WriteFlat; gasWanted: 300000, gasUsed: 301934: out of gas")
	
	output := buf.String()
	
	// Should be at debug level
	assert.Contains(t, output, "DBG", "Should be at DEBUG level")
	assert.NotContains(t, output, "INF", "Should not be at INFO level")
	
	// Should preserve module context
	assert.Contains(t, output, "module=server", "Module context should be preserved")
	
	// Should contain the error
	assert.Contains(t, output, "out of gas", "Error should be present")
}

// TestBackwardsCompatibility ensures that all other logging continues to work normally
func TestBackwardsCompatibility(t *testing.T) {
	var buf bytes.Buffer
	logger := log.NewLogger(&buf, log.ColorOption(false))
	wrappedLogger := NewTxErrorLoggerWrapper(logger)
	
	// Test normal INFO logging
	buf.Reset()
	wrappedLogger.Info("normal info message", "key", "value")
	output := buf.String()
	assert.Contains(t, output, "INF", "Normal info messages should remain at INFO level")
	assert.Contains(t, output, "normal info message")
	
	// Test DEBUG logging
	buf.Reset()
	wrappedLogger.Debug("debug message", "key", "value")
	output = buf.String()
	assert.Contains(t, output, "DBG", "Debug messages should work normally")
	
	// Test ERROR logging
	buf.Reset()
	wrappedLogger.Error("error message", "key", "value")
	output = buf.String()
	assert.Contains(t, output, "ERR", "Error messages should work normally")
	
	// Test WARN logging
	buf.Reset()
	wrappedLogger.Warn("warn message", "key", "value")
	output = buf.String()
	assert.Contains(t, output, "WRN", "Warn messages should work normally")
}

// TestExactIssueScenario replicates the exact logging scenario from issue #5765
func TestExactIssueScenario(t *testing.T) {
	originalErrorLogs := []string{
		"out of gas in location: WriteFlat; gasWanted: 300000, gasUsed: 301934: out of gas",
		"out of gas in location: WritePerByte; gasWanted: 300000, gasUsed: 300033: out of gas", 
		"out of gas in location: WriteFlat; gasWanted: 300000, gasUsed: 300121: out of gas",
	}
	
	for i, errorLog := range originalErrorLogs {
		t.Run(fmt.Sprintf("Issue error %d", i+1), func(t *testing.T) {
			var buf bytes.Buffer
			logger := log.NewLogger(&buf, log.ColorOption(false))
			
			// Use the exact same logger setup as the application
			wrappedLogger := NewTxErrorLoggerWrapper(logger).With("module", "server")
			
			// This simulates the exact call from cosmos-sdk baseapp.go:
			// ctx.Logger().Info("panic recovered in runTx", "err", err)
			wrappedLogger.Info("panic recovered in runTx", "err", errorLog)
			
			output := buf.String()
			
			// Before the fix: would contain "INF panic recovered in runTx err=..."
			// After the fix: should contain "DBG panic recovered in runTx err=..."
			if !strings.Contains(output, "DBG") {
				t.Errorf("Expected DEBUG level log, but got: %s", output)
			}
			
			if strings.Contains(output, "INF") {
				t.Errorf("Log should not be at INFO level, but got: %s", output)
			}
			
			// Verify all components are present
			require.Contains(t, output, "panic recovered in runTx", "Message should be present")
			require.Contains(t, output, errorLog, "Original error should be present")
			require.Contains(t, output, "module=server", "Module context should be present")
		})
	}
}