package docker_e2e

import (
	"context"
	"github.com/celestiaorg/celestia-app/v4/app"
	"github.com/celestiaorg/celestia-app/v4/test/util/testnode"
	"github.com/cosmos/cosmos-sdk/types/module/testutil"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
	"testing"
)
import tastoradocker "github.com/celestiaorg/tastora/framework/docker"

func TestChainBuilder(t *testing.T) {

	val := tastoradocker.NewNodeConfigBuilder().WithImage(tastoradocker.DockerImage{
		Repository: "ghcr.io/celestiaorg/celestia-app",
		Version:    "v4.0.0-rc6",
		UIDGID:     "10001:10001",
	}).WithAdditionalStartArgs([]string{"--force-no-bbr", "--grpc.enable", "--grpc.address", "0.0.0.0:9090", "--rpc.grpc_laddr=tcp://0.0.0.0:9099"}...).
		Build()

	// create validator for genesis type, has not yet added anything to keyring keyring.
	//validator := genesis2.NewDefaultValidator(testnode.DefaultValidatorAccountName)

	g := testnode.DefaultConfig().Genesis
	// this will generate gentx transactions for the validator above.
	genesisBz, err := g.ExportBytes()
	require.NoError(t, err)

	encodingConfig := testutil.MakeTestEncodingConfig(app.ModuleEncodingRegisters...)

	client, network := tastoradocker.DockerSetup(t)

	chain, err := tastoradocker.NewChainBuilder(t).
		WithLogger(zaptest.NewLogger(t)).
		WithChainID("celestia").
		WithDockerClient(client).
		WithDockerNetworkID(network).
		WithEncodingConfig(&encodingConfig).
		WithValidators(val).
		WithGenesis(genesisBz).
		Build(context.TODO())

	require.NoError(t, err)

	err = chain.Start(context.TODO())
	require.NoError(t, err)

}
