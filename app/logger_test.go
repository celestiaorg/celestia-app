package app

import (
	"bytes"
	"testing"

	"cosmossdk.io/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTxErrorLoggerWrapper(t *testing.T) {
	tests := []struct {
		name           string
		msg            string
		keyvals        []interface{}
		expectDebug    bool
		expectInfo     bool
		description    string
	}{
		{
			name:        "out of gas error should be debug",
			msg:         "panic recovered in runTx",
			keyvals:     []interface{}{"err", "out of gas in location: WriteFlat; gasWanted: 300000, gasUsed: 301934: out of gas"},
			expectDebug: true,
			expectInfo:  false,
			description: "Gas exhaustion errors should be logged at debug level",
		},
		{
			name:        "other panic should remain info",
			msg:         "panic recovered in runTx",
			keyvals:     []interface{}{"err", "some other error"},
			expectDebug: false,
			expectInfo:  true,
			description: "Non-gas errors should remain at info level",
		},
		{
			name:        "different message should remain info",
			msg:         "some other message",
			keyvals:     []interface{}{"err", "out of gas in location: WriteFlat"},
			expectDebug: false,
			expectInfo:  true,
			description: "Different messages should not be affected",
		},
		{
			name:        "panic without error key should remain info",
			msg:         "panic recovered in runTx",
			keyvals:     []interface{}{"other", "value"},
			expectDebug: false,
			expectInfo:  true,
			description: "Panic logs without err key should remain at info level",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a buffer to capture log output
			var buf bytes.Buffer
			logger := log.NewLogger(&buf, log.ColorOption(false))
			wrapper := NewTxErrorLoggerWrapper(logger)

			// Call Info on the wrapper
			wrapper.Info(tt.msg, tt.keyvals...)

			output := buf.String()

			if tt.expectDebug {
				assert.Contains(t, output, "DBG", "Expected DEBUG level log")
				assert.NotContains(t, output, "INF", "Should not contain INFO level log")
			}

			if tt.expectInfo {
				assert.Contains(t, output, "INF", "Expected INFO level log")
				assert.NotContains(t, output, "DBG", "Should not contain DEBUG level log")
			}

			// Verify the message and error are still present
			if len(tt.keyvals) >= 2 {
				assert.Contains(t, output, tt.msg, "Message should be present in output")
				if errVal, ok := tt.keyvals[1].(string); ok {
					assert.Contains(t, output, errVal, "Error should be present in output")
				}
			}
		})
	}
}

func TestTxErrorLoggerWrapper_OtherMethods(t *testing.T) {
	var buf bytes.Buffer
	logger := log.NewLogger(&buf, log.ColorOption(false))
	wrapper := NewTxErrorLoggerWrapper(logger)

	// Test that other methods pass through correctly
	wrapper.Debug("debug message", "key", "value")
	assert.Contains(t, buf.String(), "debug message")
	assert.Contains(t, buf.String(), "DBG")

	buf.Reset()
	wrapper.Error("error message", "key", "value")
	assert.Contains(t, buf.String(), "error message")
	assert.Contains(t, buf.String(), "ERR")

	buf.Reset()
	wrapper.Warn("warn message", "key", "value")
	assert.Contains(t, buf.String(), "warn message")
	assert.Contains(t, buf.String(), "WRN")
}

func TestTxErrorLoggerWrapper_With(t *testing.T) {
	var buf bytes.Buffer
	logger := log.NewLogger(&buf, log.ColorOption(false))
	wrapper := NewTxErrorLoggerWrapper(logger)

	// Test With method returns a wrapped logger
	childWrapper := wrapper.With("module", "test")
	require.IsType(t, &TxErrorLoggerWrapper{}, childWrapper)

	// Test that the child wrapper also works correctly
	childWrapper.Info("panic recovered in runTx", "err", "out of gas in location: WriteFlat")
	assert.Contains(t, buf.String(), "DBG", "Child wrapper should also downgrade gas errors")
	assert.Contains(t, buf.String(), "module=test", "Child wrapper should include additional context")
}

func TestIsGasError(t *testing.T) {
	wrapper := &TxErrorLoggerWrapper{}

	tests := []struct {
		name     string
		keyvals  []interface{}
		expected bool
	}{
		{
			name:     "out of gas error",
			keyvals:  []interface{}{"err", "out of gas in location: WriteFlat"},
			expected: true,
		},
		{
			name:     "gas error with additional context",
			keyvals:  []interface{}{"err", "out of gas in location: WritePerByte; gasWanted: 300000, gasUsed: 300033: out of gas"},
			expected: true,
		},
		{
			name:     "non-gas error",
			keyvals:  []interface{}{"err", "invalid transaction"},
			expected: false,
		},
		{
			name:     "no err key",
			keyvals:  []interface{}{"other", "out of gas"},
			expected: false,
		},
		{
			name:     "non-string error value",
			keyvals:  []interface{}{"err", 123},
			expected: false,
		},
		{
			name:     "empty keyvals",
			keyvals:  []interface{}{},
			expected: false,
		},
		{
			name:     "odd number of keyvals",
			keyvals:  []interface{}{"err"},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := wrapper.isGasError(tt.keyvals...)
			assert.Equal(t, tt.expected, result)
		})
	}
}