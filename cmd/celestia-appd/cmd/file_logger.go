package cmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"cosmossdk.io/log"
	"github.com/cosmos/cosmos-sdk/server"
	"github.com/spf13/cobra"
)

const (
	FlagEnableDebugLog = "enable-debug-log"
	FlagLogDir         = "log-dir"
)

type FileLogConfig struct {
	EnableDebugLog bool
	LogDir         string
}

func DefaultFileLogConfig(homeDir string) FileLogConfig {
	return FileLogConfig{
		EnableDebugLog: false,
		LogDir:         filepath.Join(homeDir, "logs"),
	}
}

func createLogFile(logDir, prefix string) (*os.File, error) {
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create log directory: %w", err)
	}

	timestamp := time.Now().Format("2006-01-02")
	logFileName := fmt.Sprintf("%s-%s.log", prefix, timestamp)
	logFilePath := filepath.Join(logDir, logFileName)

	file, err := os.OpenFile(logFilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to create log file: %w", err)
	}

	return file, nil
}

func setupFileLogger(cmd *cobra.Command, config FileLogConfig) error {
	sctx := server.GetServerContextFromCmd(cmd)

	debugLogFile, err := createLogFile(config.LogDir, "debug")
	if err != nil {
		return err
	}

	multiWriter := io.MultiWriter(os.Stderr, debugLogFile)
	logger := log.NewLogger(multiWriter)

	sctx.Logger = logger
	return server.SetCmdServerContext(cmd, sctx)
}

func getFileLogConfigFromFlags(cmd *cobra.Command, homeDir string) (FileLogConfig, error) {
	config := DefaultFileLogConfig(homeDir)

	var err error
	if config.EnableDebugLog, err = cmd.Flags().GetBool(FlagEnableDebugLog); err != nil {
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

	if !config.EnableDebugLog {
		return nil
	}

	return setupFileLogger(cmd, config)
}
