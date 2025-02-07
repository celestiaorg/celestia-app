package testnode

import (
	"os"

	"cosmossdk.io/log"
	"github.com/rs/zerolog"
)

func NewLogger(config *UniversalTestingConfig) log.Logger {
	if config.SuppressLogs {
		return log.NewNopLogger()
	}
	logger := log.NewLogger(os.Stdout)
	switch config.TmConfig.LogLevel {
	case "error":
		return log.NewLogger(os.Stdout, log.LevelOption(zerolog.ErrorLevel))
	case "info":
		return log.NewLogger(os.Stdout, log.LevelOption(zerolog.InfoLevel))
	case "debug":
		return log.NewLogger(os.Stdout, log.LevelOption(zerolog.DebugLevel))
	default:
		return logger
	}
}
