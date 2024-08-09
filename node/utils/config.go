package utils

import (
	v1 "github.com/celestiaorg/celestia-app/v2/pkg/appconsts/v1"
	"github.com/celestiaorg/celestia-app/v2/test/util/testnode"
)

func GetConfig() *testnode.Config {
	tmConfig := testnode.DefaultTendermintConfig()
	tmConfig.LogLevel = "error"
	tmConfig.P2P.ListenAddress = "tcp://127.0.0.1:26656"
	tmConfig.RPC.ListenAddress = "tcp://127.0.0.1:26657"
	tmConfig.RPC.GRPCListenAddress = "tcp://127.0.0.1:26658"

	consensusParams := testnode.DefaultConsensusParams()
	consensusParams.Version.AppVersion = v1.Version

	return testnode.DefaultConfig().
		WithTendermintConfig(tmConfig).
		WithConsensusParams(consensusParams).
		WithSuppressLogs(false)
}
