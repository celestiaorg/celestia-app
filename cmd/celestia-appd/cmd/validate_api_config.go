package cmd

import (
	"errors"

	"cosmossdk.io/log"
	"github.com/cosmos/cosmos-sdk/server"
	"github.com/spf13/cobra"
)

// validateAPIConfig ensures the REST API's gRPC backend is enabled.
func validateAPIConfig(cmd *cobra.Command, _ log.Logger) error {
	v := server.GetServerContextFromCmd(cmd).Viper
	if v.GetBool(server.FlagAPIEnable) && !v.GetBool("grpc.enable") {
		return errors.New("REST API requires Cosmos SDK gRPC; enable grpc.enable in app.toml or pass --grpc.enable=true")
	}
	return nil
}
