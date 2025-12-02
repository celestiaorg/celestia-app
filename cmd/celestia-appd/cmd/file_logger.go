package cmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"cosmossdk.io/log"
	"github.com/cosmos/cosmos-sdk/server"
	"github.com/spf13/cobra"
	"gopkg.in/natefinch/lumberjack.v2"
)

const (
	FlagDisableDebugLog = "disable-debug-log"
	FlagLogDir          = "log-dir"
)

type FileLogConfig struct {
	DisableDebugLog bool
	LogDir          string
	MaxSize         int
	MaxBackups      int
}

func DefaultFileLogConfig(homeDir string) FileLogConfig {
	return FileLogConfig{
		DisableDebugLog: false,
		LogDir:          filepath.Join(homeDir, "logs"),
		MaxSize:         20,
		MaxBackups:      5,
	}
}

func setupFileLogger(cmd *cobra.Command, config FileLogConfig) error {
	sctx := server.GetServerContextFromCmd(cmd)

	if err := os.MkdirAll(config.LogDir, 0o755); err != nil {
		return fmt.Errorf("failed to create log directory: %w", err)
	}

	logFile := &lumberjack.Logger{
		Filename:   filepath.Join(config.LogDir, "debug.log"),
		MaxSize:    config.MaxSize,
		MaxBackups: config.MaxBackups,
		Compress:   true,
	}

	multiWriter := io.MultiWriter(os.Stderr, logFile)
	logger := log.NewLogger(multiWriter)

	sctx.Logger = logger
	return server.SetCmdServerContext(cmd, sctx)
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

	return config, nil
}

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
