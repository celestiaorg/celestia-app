package utils

import (
	v1 "github.com/celestiaorg/celestia-app/v3/pkg/appconsts/v1"
	"github.com/celestiaorg/celestia-app/v3/test/util/testnode"
)

func GetConfig() *testnode.Config {
	tmConfig := testnode.DefaultTendermintConfig()
	tmConfig.LogLevel = "DEBUG"
	tmConfig.P2P.ListenAddress = "tcp://127.0.0.1:26656"
	tmConfig.RPC.ListenAddress = "tcp://127.0.0.1:26657"
	tmConfig.RPC.GRPCListenAddress = "tcp://127.0.0.1:26658"
	tmConfig.RootDir = "/Users/rootulp/.celestia-app"

	consensusParams := testnode.DefaultConsensusParams()
	consensusParams.Version.AppVersion = v1.Version // start the prototype on v1

	return testnode.DefaultConfig().
		WithTendermintConfig(tmConfig).
		WithConsensusParams(consensusParams).
		WithSuppressLogs(false)
}
