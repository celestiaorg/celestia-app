package testnode

import (
	"os"

	"github.com/tendermint/tendermint/libs/log"
)

func NewLogger(config *UniversalTestingConfig) log.Logger {
	if config.SuppressLogs {
		return log.NewNopLogger()
	}
	logger := log.NewTMLogger(log.NewSyncWriter(os.Stdout))
	switch config.TmConfig.LogLevel {
	case "error":
		return log.NewFilter(logger, log.AllowError())
	case "info":
		return log.NewFilter(logger, log.AllowInfo())
	case "debug":
		return log.NewFilter(logger, log.AllowDebug())
	default:
		return logger
	}
}
