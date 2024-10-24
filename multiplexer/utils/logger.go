package utils

import (
	"os"

	"github.com/celestiaorg/celestia-app/v3/test/util/testnode"
	"github.com/tendermint/tendermint/libs/log"
)

func NewLogger(config *testnode.UniversalTestingConfig) log.Logger {
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
