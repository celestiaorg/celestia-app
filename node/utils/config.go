package utils

import "github.com/celestiaorg/celestia-app/v2/test/util/testnode"

func GetConfig() *testnode.Config {
	tmConfig := testnode.DefaultTendermintConfig()
	tmConfig.LogLevel = "DEBUG"
	tmConfig.P2P.ListenAddress = "tcp://127.0.0.1:26656"
	tmConfig.RPC.ListenAddress = "tcp://127.0.0.1:26657"
	tmConfig.RPC.GRPCListenAddress = "tcp://127.0.0.1:26658"
	return testnode.DefaultConfig().WithTendermintConfig(tmConfig).WithSuppressLogs(false)
}
