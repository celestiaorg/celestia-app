package docker_e2e

import (
	"context"
	"github.com/celestiaorg/celestia-app/v4/app"
	"github.com/celestiaorg/celestia-app/v4/test/util/genesis"
	"github.com/celestiaorg/celestia-app/v4/test/util/testnode"
	"github.com/celestiaorg/tastora/framework/testutil/maps"
	"github.com/celestiaorg/tastora/framework/testutil/wait"
	cmtjson "github.com/cometbft/cometbft/libs/json"
	"github.com/cometbft/cometbft/privval"
	"github.com/cosmos/cosmos-sdk/types/module/testutil"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
	"testing"
)
import tastoradocker "github.com/celestiaorg/tastora/framework/docker"

func TestChainBuilder(t *testing.T) {

	g := testnode.DefaultConfig().Genesis.WithChainID("celestia")

	genesisBz, err := g.ExportBytes()
	require.NoError(t, err, "failed to export genesis bytes")

	// TODO: why do I need to do this?
	genesisBz, err = maps.SetField(genesisBz, "consensus", map[string]interface{}{})
	require.NoError(t, err)
	genesisBz, err = maps.SetField(genesisBz, "consensus.params.version.app", "4")
	require.NoError(t, err)

	encodingConfig := testutil.MakeTestEncodingConfig(app.ModuleEncodingRegisters...)

	client, network := tastoradocker.DockerSetup(t)

	chain, err := tastoradocker.NewChainBuilder(t).
		WithName("celestia").
		WithGenesisKeyring(g.Keyring()).
		WithPrivValidatorKey(getValidatorPrivateKeyBytes(t, g, 0)).
		WithLogger(zaptest.NewLogger(t)).
		WithChainID(g.ChainID).
		WithDockerClient(client).
		WithDockerNetworkID(network).
		WithEncodingConfig(&encodingConfig).
		WithValidators(
			// specify validators for the chain. In this case a single validator.
			tastoradocker.NewNodeConfigBuilder().
				WithImage(tastoradocker.NewDockerImage("ghcr.io/celestiaorg/celestia-app", "v4.0.4-alpha", "10001:10001")).
				WithAdditionalStartArgs("--force-no-bbr", "--grpc.enable", "--grpc.address", "0.0.0.0:9090", "--rpc.grpc_laddr=tcp://0.0.0.0:9099").
				Build(),
		).
		WithGenesis(genesisBz).
		Build(context.TODO()) // creates and initializes underlying docker resources with provided spec.

	require.NoError(t, err)

	err = chain.Start(context.TODO())
	require.NoError(t, err)

	err = wait.ForBlocks(context.TODO(), 5, chain)
	require.NoError(t, err)

}

// getValidatorPrivateKeyBytes returns the contents of the priv_validator_key.json file.
func getValidatorPrivateKeyBytes(t *testing.T, genesis *genesis.Genesis, idx int) []byte {
	validator, exists := genesis.Validator(idx)
	require.True(t, exists, "validator at index 0 should exist")
	privValKey := validator.ConsensusKey

	key := privval.FilePVKey{
		Address: privValKey.PubKey().Address(),
		PubKey:  privValKey.PubKey(),
		PrivKey: privValKey,
	}

	privValidatorKeyBz, err := cmtjson.MarshalIndent(key, "", "  ")
	require.NoError(t, err, "failed to marshal priv_validator_key.json")
	return privValidatorKeyBz
}
