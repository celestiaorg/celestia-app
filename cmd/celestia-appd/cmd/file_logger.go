package cmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"

	"cosmossdk.io/log"
	"github.com/cosmos/cosmos-sdk/server"
	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
	"gopkg.in/natefinch/lumberjack.v2"
)

const (
	FlagDisableDebugLog = "disable-debug-log"
	FlagLogDir          = "log-dir"
	FlagLogMaxSize      = "log-max-size"
	FlagLogMaxBackups   = "log-max-backups"
)

type FileLogConfig struct {
	DisableDebugLog bool
	LogDir          string
	// MaxSize is the max size of each log file in MB
	MaxSize int
	// MaxBackups is the number of historical log files to keep
	MaxBackups int
}

func DefaultFileLogConfig(homeDir string) FileLogConfig {
	return FileLogConfig{
		DisableDebugLog: false,
		LogDir:          filepath.Join(homeDir, "logs"),
		MaxSize:         20,
		MaxBackups:      5,
	}
}

// ansiColorFilter is a writer that strips ANSI color codes from the output
type ansiColorFilter struct {
	writer io.Writer
	ansiRegex *regexp.Regexp
}

func newAnsiColorFilter(writer io.Writer) *ansiColorFilter {
	// Regex to match ANSI escape sequences
	return &ansiColorFilter{
		writer: writer,
		ansiRegex: regexp.MustCompile(`\x1b\[[0-9;]*m`),
	}
}

func (f *ansiColorFilter) Write(p []byte) (n int, err error) {
	// Strip ANSI color codes
	cleaned := f.ansiRegex.ReplaceAll(p, []byte{})
	return f.writer.Write(cleaned)
}

// setupFileLogger sets up dual logging: console with configured level, file with debug level
func setupFileLogger(cmd *cobra.Command, config FileLogConfig) error {
	sctx := server.GetServerContextFromCmd(cmd)

	if err := os.MkdirAll(config.LogDir, 0o755); err != nil {
		return fmt.Errorf("failed to create log directory: %w", err)
	}

	// Setup file logger for debug logs
	logFile := &lumberjack.Logger{
		Filename:   filepath.Join(config.LogDir, "debug.log"),
		MaxSize:    config.MaxSize,
		MaxBackups: config.MaxBackups,
		Compress:   true,
	}

	// Create filtered writer to strip ANSI codes from file output
	filteredFileWriter := newAnsiColorFilter(logFile)

	// Create a tee writer that duplicates output to both stderr and file
	teeWriter := io.MultiWriter(os.Stderr, filteredFileWriter)

	// Create logger that writes to both destinations
	// The logger level is set based on the command line flag
	// File will get ALL logs that pass the logger's filter
	// Console will also get the same logs
	var loggerOpts []log.Option
	
	// Check if we should use debug level for file logging
	// If console is set to info or higher, we need to ensure file still gets debug
	consoleLevel := sctx.Config.LogLevel
	if consoleLevel == "" {
		consoleLevel = "info" // default
	}
	
	// For file to capture debug logs, we need the logger itself to be at debug level
	// But then we need to filter console output separately
	if consoleLevel != "debug" && consoleLevel != "trace" {
		// Use custom writer that filters console output by level
		teeWriter = &levelFilteredWriter{
			consoleWriter: os.Stderr,
			fileWriter:    filteredFileWriter,
			consoleLevel:  consoleLevel,
		}
		// Set logger to debug to capture all for file
		loggerOpts = append(loggerOpts, log.LevelOption(zerolog.DebugLevel))
	} else {
		// If console is already at debug, just use the tee writer
		loggerOpts = append(loggerOpts, log.LevelOption(zerolog.DebugLevel))
	}
	
	loggerOpts = append(loggerOpts, log.ColorOption(true)) // Color for console
	logger := log.NewLogger(teeWriter, loggerOpts...)

	sctx.Logger = logger
	return server.SetCmdServerContext(cmd, sctx)
}

// levelFilteredWriter writes to file (all logs) and console (filtered by level)
type levelFilteredWriter struct {
	consoleWriter io.Writer
	fileWriter    io.Writer
	consoleLevel  string
}

func (w *levelFilteredWriter) Write(p []byte) (n int, err error) {
	// Always write to file (gets all debug logs)
	if _, err := w.fileWriter.Write(p); err != nil {
		return 0, err
	}

	// Filter console output based on configured log level
	logStr := string(p)
	shouldWriteToConsole := true

	// Parse log level from the log line
	// Common formats: DBG, DEBUG, TRC, TRACE, INF, INFO, WRN, WARN, ERR, ERROR
	switch w.consoleLevel {
	case "error":
		// Only show errors
		if !regexp.MustCompile(`(?i)\bERR\b|\bERROR\b|\bFATAL\b`).MatchString(logStr) {
			shouldWriteToConsole = false
		}
	case "warn", "warning":
		// Show warnings and errors
		if !regexp.MustCompile(`(?i)\bWRN\b|\bWARN\b|\bWARNING\b|\bERR\b|\bERROR\b|\bFATAL\b`).MatchString(logStr) {
			shouldWriteToConsole = false
		}
	case "info":
		// Show info, warnings, and errors (filter out debug and trace)
		if regexp.MustCompile(`(?i)\bDBG\b|\bDEBUG\b|\bTRC\b|\bTRACE\b`).MatchString(logStr) {
			shouldWriteToConsole = false
		}
	case "debug":
		// Show everything except trace
		if regexp.MustCompile(`(?i)\bTRC\b|\bTRACE\b`).MatchString(logStr) {
			shouldWriteToConsole = false
		}
	// For "trace" or any other value, show everything
	}

	if shouldWriteToConsole {
		if _, err := w.consoleWriter.Write(p); err != nil {
			return 0, err
		}
	}

	return len(p), nil
}

func getFileLogConfigFromFlags(cmd *cobra.Command, homeDir string) (FileLogConfig, error) {
	config := DefaultFileLogConfig(homeDir)

	var err error
	if config.DisableDebugLog, err = cmd.Flags().GetBool(FlagDisableDebugLog); err != nil {
		return config, err
	}
	if config.LogDir, err = cmd.Flags().GetString(FlagLogDir); err != nil {
		return config, err
	}
	if config.MaxSize, err = cmd.Flags().GetInt(FlagLogMaxSize); err != nil {
		return config, err
	}
	if config.MaxBackups, err = cmd.Flags().GetInt(FlagLogMaxBackups); err != nil {
		return config, err
	}

	return config, nil
}

// replaceLoggerWithFileSupport optionally replaces the logger with a file logger if the flag
// is set. This function is called as part of the PersistentPreRunE hook.
func replaceLoggerWithFileSupport(cmd *cobra.Command, homeDir string) error {
	logFilePath, err := cmd.Flags().GetString(FlagLogToFile)
	if err != nil {
		return err
	}

	if logFilePath != "" {
		return replaceLogger(cmd)
	}

	config, err := getFileLogConfigFromFlags(cmd, homeDir)
	if err != nil {
		return err
	}

	if config.DisableDebugLog {
		return nil
	}

	return setupFileLogger(cmd, config)
}
