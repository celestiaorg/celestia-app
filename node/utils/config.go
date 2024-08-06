package utils

import "github.com/celestiaorg/celestia-app/v2/test/util/testnode"

func GetConfig() *testnode.Config {
	tmConfig := testnode.DefaultTendermintConfig()
	tmConfig.LogLevel = "DEBUG"
	return testnode.DefaultConfig().WithTendermintConfig(tmConfig).WithSuppressLogs(false)
}
