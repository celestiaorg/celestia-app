package docker_e2e

import (
	"context"
	"github.com/celestiaorg/celestia-app/v4/app"
	"github.com/celestiaorg/celestia-app/v4/test/util/testnode"
	"github.com/celestiaorg/tastora/framework/testutil/maps"
	"github.com/celestiaorg/tastora/framework/testutil/wait"
	"github.com/cometbft/cometbft/privval"
	"github.com/cosmos/cosmos-sdk/types/module/testutil"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
	"os"
	"testing"
)
import tastoradocker "github.com/celestiaorg/tastora/framework/docker"

func TestChainBuilder(t *testing.T) {

	val := tastoradocker.NewNodeConfigBuilder().
		WithImage(tastoradocker.DockerImage{
			Repository: "ghcr.io/celestiaorg/celestia-app",
			Version:    "v4.0.4-alpha",
			UIDGID:     "10001:10001",
		},
		).WithAdditionalStartArgs([]string{"--force-no-bbr", "--grpc.enable", "--grpc.address", "0.0.0.0:9090", "--rpc.grpc_laddr=tcp://0.0.0.0:9099"}...).
		Build()

	// create validator for genesis type, has not yet added anything to keyring keyring.
	//validator := genesis2.NewDefaultValidator(testnode.DefaultValidatorAccountName)

	g := testnode.DefaultConfig().Genesis.WithChainID("celestia")
	// this will generate gentx transactions for the validator above.
	genesisBz, err := g.ExportBytes()
	require.NoError(t, err)

	// TODO: why do I need to do this?
	genesisBz, err = maps.SetField(genesisBz, "consensus", map[string]interface{}{})
	require.NoError(t, err)
	genesisBz, err = maps.SetField(genesisBz, "consensus.params.version.app", "4")
	require.NoError(t, err)

	encodingConfig := testutil.MakeTestEncodingConfig(app.ModuleEncodingRegisters...)

	client, network := tastoradocker.DockerSetup(t)

	// Get the private validator key from the testnode configuration
	validator, exists := g.Validator(0) // Get validator at index 0
	require.True(t, exists, "validator at index 0 should exist")
	privValKey := validator.ConsensusKey

	// Create temporary files for both key and state
	tempKeyFile, err := os.CreateTemp("", "priv_validator_key_*.json")
	require.NoError(t, err)
	defer os.Remove(tempKeyFile.Name())
	defer tempKeyFile.Close()

	tempStateFile, err := os.CreateTemp("", "priv_validator_state_*.json")
	require.NoError(t, err)
	defer os.Remove(tempStateFile.Name())
	defer tempStateFile.Close()

	// Create the FilePV and save it to get the correct format
	filePV := privval.NewFilePV(privValKey, tempKeyFile.Name(), tempStateFile.Name())
	filePV.Save()

	// Read the generated key file content
	privValidatorKeyBz, err := os.ReadFile(tempKeyFile.Name())
	require.NoError(t, err)

	// Debug: print the key content
	t.Logf("Private validator key JSON: %s", string(privValidatorKeyBz))

	chain, err := tastoradocker.NewChainBuilder(t).
		WithName("celestia").
		WithGenesisKeyring(g.Keyring()).
		WithPrivValidatorKey(privValidatorKeyBz).
		WithLogger(zaptest.NewLogger(t)).
		WithChainID(g.ChainID).
		WithDockerClient(client).
		WithDockerNetworkID(network).
		WithEncodingConfig(&encodingConfig).
		WithValidators(val).
		WithGenesis(genesisBz).
		Build(context.TODO())

	require.NoError(t, err)

	err = chain.Start(context.TODO())
	require.NoError(t, err)

	err = wait.ForBlocks(context.TODO(), 5, chain)
	require.NoError(t, err)

}
