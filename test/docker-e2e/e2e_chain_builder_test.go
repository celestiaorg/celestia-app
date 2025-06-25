package docker_e2e

import (
	"context"
	sdkmath "cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app/v4/app"
	"github.com/celestiaorg/celestia-app/v4/app/encoding"
	"github.com/celestiaorg/celestia-app/v4/pkg/user"
	"github.com/celestiaorg/celestia-app/v4/test/util/genesis"
	"github.com/celestiaorg/celestia-app/v4/test/util/testnode"
	tastoradocker "github.com/celestiaorg/tastora/framework/docker"
	"github.com/celestiaorg/tastora/framework/testutil/maps"
	"github.com/celestiaorg/tastora/framework/testutil/wait"
	cmtjson "github.com/cometbft/cometbft/libs/json"
	"github.com/cometbft/cometbft/privval"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module/testutil"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
	"testing"
)

func TestChainBuilder(t *testing.T) {

	// default + 2 extra validators.
	g := testnode.DefaultConfig().Genesis.WithChainID("celestia").WithValidators(
		genesis.NewDefaultValidator("val1"),
		genesis.NewDefaultValidator("val2"),
	)

	genesisBz, err := g.ExportBytes()
	require.NoError(t, err, "failed to export genesis bytes")

	// TODO: why do I need to do this?
	genesisBz, err = maps.SetField(genesisBz, "consensus", map[string]interface{}{})
	require.NoError(t, err)
	genesisBz, err = maps.SetField(genesisBz, "consensus.params.version.app", "4")
	require.NoError(t, err)

	encodingConfig := testutil.MakeTestEncodingConfig(app.ModuleEncodingRegisters...)

	client, network := tastoradocker.DockerSetup(t)

	kr := g.Keyring()

	chain, err := tastoradocker.NewChainBuilder(t).
		WithName("celestia"). // just influences home directory on the host.
		WithGenesisKeyring(kr). // provide the keyring which contains all the keys already generated.
		WithLogger(zaptest.NewLogger(t)).
		WithChainID(g.ChainID).
		WithDockerClient(client).
		WithDockerNetworkID(network).
		WithEncodingConfig(&encodingConfig).
		WithValidators(
			// specify validators for the chain.
			tastoradocker.NewChainNodeConfigBuilder().
				WithImage(tastoradocker.NewDockerImage("ghcr.io/celestiaorg/celestia-app", "v4.0.4-alpha", "10001:10001")).
				WithAdditionalStartArgs("--force-no-bbr", "--grpc.enable", "--grpc.address", "0.0.0.0:9090", "--rpc.grpc_laddr=tcp://0.0.0.0:9099").
				WithPrivValidatorKey(getValidatorPrivateKeyBytes(t, g, 0)).
				Build(),
			tastoradocker.NewChainNodeConfigBuilder().
				WithImage(tastoradocker.NewDockerImage("ghcr.io/celestiaorg/celestia-app", "v4.0.4-alpha", "10001:10001")).
				WithAdditionalStartArgs("--force-no-bbr", "--grpc.enable", "--grpc.address", "0.0.0.0:9090", "--rpc.grpc_laddr=tcp://0.0.0.0:9099").
				WithPrivValidatorKey(getValidatorPrivateKeyBytes(t, g, 1)).
				Build(),
			tastoradocker.NewChainNodeConfigBuilder().
				WithImage(tastoradocker.NewDockerImage("ghcr.io/celestiaorg/celestia-app", "v4.0.4-alpha", "10001:10001")).
				WithAdditionalStartArgs("--force-no-bbr", "--grpc.enable", "--grpc.address", "0.0.0.0:9090", "--rpc.grpc_laddr=tcp://0.0.0.0:9099").
				WithPrivValidatorKey(getValidatorPrivateKeyBytes(t, g, 2)).
				Build(),
		).
		WithGenesis(genesisBz).
		Build(context.TODO()) // creates and initializes underlying docker resources with provided spec.

	require.NoError(t, err)

	err = chain.Start(context.TODO())
	require.NoError(t, err)

	err = wait.ForBlocks(context.TODO(), 5, chain)
	require.NoError(t, err)

	// test sending a transaction to verify the validator is working properly
	testBankSend(t, kr, chain)
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

func testBankSend(t *testing.T, kr keyring.Keyring, chain *tastoradocker.Chain) {
	ctx := context.Background()

	recipientWallet, err := chain.CreateWallet(ctx, "recipient")
	require.NoError(t, err, "failed to create recipient wallet")

	txClient, err := setupTxClient(ctx, kr, chain)
	require.NoError(t, err, "failed to setup TxClient")

	// get the default account address from TxClient (should be validator)
	fromAddr := txClient.DefaultAddress()
	toAddr, err := sdk.AccAddressFromBech32(recipientWallet.GetFormattedAddress())
	require.NoError(t, err, "failed to parse recipient address")

	t.Logf("Validator address: %s", fromAddr.String())
	t.Logf("Recipient address: %s", toAddr.String())

	sendAmount := sdk.NewCoins(sdk.NewCoin("utia", sdkmath.NewInt(1000000))) // 1 TIA
	msg := banktypes.NewMsgSend(fromAddr, toAddr, sendAmount)

	// Submit transaction using TxClient with proper minimum fee
	// Required: 0.025utia per gas unit, so 200000 * 0.025 = 5000 utia minimum
	txResp, err := txClient.SubmitTx(ctx, []sdk.Msg{msg}, user.SetGasLimit(200000), user.SetFee(5000))
	require.NoError(t, err, "failed to submit transaction")
	require.Equal(t, uint32(0), txResp.Code, "transaction failed with code %d", txResp.Code)

	t.Logf("Transaction successful! TxHash: %s, Height: %d", txResp.TxHash, txResp.Height)

	// wait for additional blocks to ensure transaction is finalized
	err = wait.ForBlocks(ctx, 2, chain)
	require.NoError(t, err, "failed to wait for blocks after transaction")
}

func setupTxClient(ctx context.Context, kr keyring.Keyring, chain *tastoradocker.Chain) (*user.TxClient, error) {
	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)

	return user.SetupTxClient(
		ctx,
		kr, // NOTE: this is the genesis keyring, not required to fetch anything from the node itself since the keys are generated in the test.
		chain.GetNode().GrpcConn,
		encCfg,
		user.WithDefaultAccount("validator"), // wse the validator account as default
	)
}
