package cmd

import (
	"cosmossdk.io/log"
	"github.com/cosmos/cosmos-sdk/server"
	"github.com/spf13/cobra"
)

// overridePrivValidatorGRPCConfig disables the privval gRPC server in non-fibre
// builds by clearing its listen address (core only starts the server when the
// address is non-empty). Its default, 127.0.0.1:26659, conflicts with the
// default TMKMS remote signing port. The fibre module needs this endpoint for
// signing, so it is left untouched in fibre builds.
func overridePrivValidatorGRPCConfig(cmd *cobra.Command, logger log.Logger) error {
	if isFibreEnabled() {
		return nil
	}

	sctx := server.GetServerContextFromCmd(cmd)
	cfg := sctx.Config

	if cfg.PrivValidatorGRPCListenAddr != "" {
		logger.Info("Disabling privval gRPC server (PrivValidatorAPI)",
			"configured", cfg.PrivValidatorGRPCListenAddr,
		)
		cfg.PrivValidatorGRPCListenAddr = ""
	}

	return nil
}
