package cmd

import (
	"bytes"
	"testing"

	"cosmossdk.io/log"
	"github.com/celestiaorg/celestia-app/v6/app"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewAppServer_LoggerWrapper(t *testing.T) {
	// Create a buffer to capture log output
	var buf bytes.Buffer
	logger := log.NewLogger(&buf, log.ColorOption(false))

	// Create a mock app server (we can't easily test the full app creation)
	// But we can test that the logger wrapper is applied correctly
	wrappedLogger := app.NewTxErrorLoggerWrapper(logger)

	// Test that gas-related panic logs are downgraded to debug
	wrappedLogger.Info("panic recovered in runTx", "err", "out of gas in location: WriteFlat; gasWanted: 300000, gasUsed: 301934: out of gas")
	
	output := buf.String()
	assert.Contains(t, output, "DBG", "Gas-related panic should be logged at DEBUG level")
	assert.NotContains(t, output, "INF", "Gas-related panic should not be logged at INFO level")

	// Reset buffer and test non-gas panic
	buf.Reset()
	wrappedLogger.Info("panic recovered in runTx", "err", "some other error")
	
	output = buf.String()
	assert.Contains(t, output, "INF", "Non-gas panic should remain at INFO level")
	assert.NotContains(t, output, "DBG", "Non-gas panic should not be at DEBUG level")
}

func TestNewAppServer_Integration(t *testing.T) {
	// Test that the NewAppServer function successfully applies the logger wrapper
	// without breaking the app creation process
	
	// We can't easily test the full app creation without a lot of setup,
	// but we can at least verify that the logger wrapper is being created correctly
	var buf bytes.Buffer
	logger := log.NewLogger(&buf, log.ColorOption(false))
	
	wrapped := app.NewTxErrorLoggerWrapper(logger)
	require.NotNil(t, wrapped, "Logger wrapper should be created successfully")
	
	// Verify it implements the interface correctly
	wrapped.Info("test", "key", "value")
	wrapped.Debug("test", "key", "value")
	wrapped.Error("test", "key", "value")
	wrapped.Warn("test", "key", "value")
	
	// Test With method
	childLogger := wrapped.With("module", "test")
	require.NotNil(t, childLogger, "With method should return a logger")
	
	// Test Impl method
	impl := wrapped.Impl()
	require.NotNil(t, impl, "Impl method should return the underlying implementation")
}