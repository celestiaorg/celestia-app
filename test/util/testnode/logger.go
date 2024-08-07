package testnode

import (
	"os"

	"github.com/tendermint/tendermint/libs/log"
)

func newLogger(cfg *UniversalTestingConfig) log.Logger {
	if cfg.SuppressLogs {
		return log.NewNopLogger()
	}
	logger := log.NewTMLogger(log.NewSyncWriter(os.Stdout))
	logger = log.NewFilter(logger, log.AllowError())
	return logger
}
