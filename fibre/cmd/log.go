package main

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/spf13/cobra"
)

const (
	flagLogLevel    = "log-level"
	envLogLevel     = "FIBRE_LOG_LEVEL"
	defaultLogLevel = "info"

	flagLogFormat    = "log-format"
	envLogFormat     = "FIBRE_LOG_FORMAT"
	defaultLogFormat = "text"
)

// setupLogging reads the log-level and log-format flags from cmd and
// configures the global slog default accordingly.
func setupLogging(cmd *cobra.Command) error {
	levelStr, err := cmd.Flags().GetString(flagLogLevel)
	if err != nil {
		return fmt.Errorf("get %q flag: %w", flagLogLevel, err)
	}
	var level slog.Level
	if err := level.UnmarshalText([]byte(levelStr)); err != nil {
		return fmt.Errorf("invalid log level %q: %w", levelStr, err)
	}

	format, err := cmd.Flags().GetString(flagLogFormat)
	if err != nil {
		return fmt.Errorf("get %q flag: %w", flagLogFormat, err)
	}

	opts := &slog.HandlerOptions{Level: level}
	var handler slog.Handler
	switch format {
	case "json":
		handler = slog.NewJSONHandler(os.Stderr, opts)
	case "text":
		handler = slog.NewTextHandler(os.Stderr, opts)
	default:
		return fmt.Errorf("invalid log format %q: must be text or json", format)
	}
	slog.SetDefault(slog.New(handler))
	return nil
}

// registerLogFlags adds the log-level and log-format persistent flags to cmd
// and applies any corresponding environment variable overrides.
func registerLogFlags(cmd *cobra.Command) {
	cmd.PersistentFlags().String(flagLogLevel, defaultLogLevel, fmt.Sprintf("log level (debug|info|warn|error) (or set %s)", envLogLevel))
	cmd.PersistentFlags().String(flagLogFormat, defaultLogFormat, fmt.Sprintf("log format (text|json) (or set %s)", envLogFormat))

	if lvl, ok := os.LookupEnv(envLogLevel); ok && lvl != "" {
		if err := cmd.PersistentFlags().Lookup(flagLogLevel).Value.Set(lvl); err != nil {
			fmt.Printf("Error setting log level from %s: %v\n", envLogLevel, err)
			os.Exit(1)
		}
	}
	if fmt_, ok := os.LookupEnv(envLogFormat); ok && fmt_ != "" {
		if err := cmd.PersistentFlags().Lookup(flagLogFormat).Value.Set(fmt_); err != nil {
			fmt.Printf("Error setting log format from %s: %v\n", envLogFormat, err)
			os.Exit(1)
		}
	}
}
