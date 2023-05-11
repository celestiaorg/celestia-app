package cmd

import (
	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/pkg/square"
	"github.com/celestiaorg/celestia-app/x/paramfilter"
	"github.com/cosmos/cosmos-sdk/server"
	"github.com/spf13/cobra"
)

// overrideServerConfig applies overrides to the embedded server context's
// configurations.
func overrideServerConfig(command *cobra.Command) error {
	ctx := server.GetServerContextFromCmd(command)
	ctx.Config.Consensus.TimeoutPropose = appconsts.TimeoutPropose
	ctx.Config.Consensus.TargetHeightDuration = appconsts.TargetHeightDuration
	ctx.Config.Consensus.SkipTimeoutCommit = false
	return server.SetCmdServerContext(command, ctx)
}

func setDefaultConsensusParams(command *cobra.Command) error {
	ctx := server.GetServerContextFromCmd(command)
	ctx.DefaultConsensusParams = paramfilter.DefaultConsensusParams()
	ctx.DefaultConsensusParams.Block.MaxBytes = square.EstimateMaxBlockBytes(64)
	return server.SetCmdServerContext(command, ctx)
}
