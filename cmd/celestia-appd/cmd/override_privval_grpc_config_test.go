//go:build !fibre

package cmd

import (
	"context"
	"testing"

	"cosmossdk.io/log"
	"github.com/celestiaorg/celestia-app/v10/app"
	"github.com/cosmos/cosmos-sdk/server"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOverridePrivValidatorGRPCConfig(t *testing.T) {
	logger := log.NewNopLogger()
	cmd := &cobra.Command{Use: "test"}

	sctx := server.NewDefaultContext()
	sctx.Config = app.DefaultConsensusConfig()
	sctx.Config.PrivValidatorGRPCListenAddr = "127.0.0.1:26659"
	sctx.Logger = logger

	ctx := context.WithValue(context.Background(), server.ServerContextKey, sctx)
	cmd.SetContext(ctx)

	require.NoError(t, overridePrivValidatorGRPCConfig(cmd, logger))

	// In non-fibre builds the privval gRPC server is disabled by clearing its
	// listen address.
	assert.Empty(t, server.GetServerContextFromCmd(cmd).Config.PrivValidatorGRPCListenAddr)
}
