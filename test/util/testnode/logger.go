package testnode

import (
	"os"

	"github.com/tendermint/tendermint/libs/log"
)

func newLogger(config *UniversalTestingConfig) log.Logger {
	if config.SuppressLogs {
		return log.NewNopLogger()
	}
	return log.NewTMLogger(log.NewSyncWriter(os.Stdout))
}
