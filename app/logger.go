package app

import (
	"strings"

	"cosmossdk.io/log"
)

// TxErrorLoggerWrapper wraps a logger to change the log level for specific transaction error messages.
// It changes "out of gas" panic recovery logs from INFO to DEBUG level.
type TxErrorLoggerWrapper struct {
	logger log.Logger
}

// NewTxErrorLoggerWrapper creates a new logger wrapper that downgrades transaction error logs.
func NewTxErrorLoggerWrapper(logger log.Logger) log.Logger {
	return &TxErrorLoggerWrapper{logger: logger}
}

// Info logs an info message, but downgrades specific transaction error messages to debug level.
func (l *TxErrorLoggerWrapper) Info(msg string, keyvals ...interface{}) {
	// Check if this is a panic recovery log with gas error
	if msg == "panic recovered in runTx" && l.isGasError(keyvals...) {
		l.logger.Debug(msg, keyvals...)
		return
	}
	l.logger.Info(msg, keyvals...)
}

// isGasError checks if the error message indicates a gas-related error
func (l *TxErrorLoggerWrapper) isGasError(keyvals ...interface{}) bool {
	for i := 0; i < len(keyvals)-1; i += 2 {
		if key, ok := keyvals[i].(string); ok && key == "err" {
			if errMsg, ok := keyvals[i+1].(string); ok {
				return strings.Contains(errMsg, "out of gas")
			}
		}
	}
	return false
}

// Debug passes through to the underlying logger
func (l *TxErrorLoggerWrapper) Debug(msg string, keyvals ...interface{}) {
	l.logger.Debug(msg, keyvals...)
}

// Error passes through to the underlying logger
func (l *TxErrorLoggerWrapper) Error(msg string, keyvals ...interface{}) {
	l.logger.Error(msg, keyvals...)
}

// Warn passes through to the underlying logger
func (l *TxErrorLoggerWrapper) Warn(msg string, keyvals ...interface{}) {
	l.logger.Warn(msg, keyvals...)
}

// With passes through to the underlying logger
func (l *TxErrorLoggerWrapper) With(keyvals ...interface{}) log.Logger {
	return &TxErrorLoggerWrapper{logger: l.logger.With(keyvals...)}
}

// Impl returns the underlying logger implementation
func (l *TxErrorLoggerWrapper) Impl() any {
	return l.logger.Impl()
}