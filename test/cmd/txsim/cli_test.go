package main

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v3/app"
	"github.com/celestiaorg/celestia-app/v3/app/encoding"
	"github.com/celestiaorg/celestia-app/v3/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v3/test/util/genesis"
	"github.com/celestiaorg/celestia-app/v3/test/util/testfactory"
	"github.com/celestiaorg/celestia-app/v3/test/util/testnode"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

func TestTxsimCommandFlags(t *testing.T) {
	_, _, grpcAddr := setup(t)
	cmd := command()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd.SetArgs([]string{
		"--key-mnemonic", testfactory.TestAccMnemo,
		"--grpc-endpoint", grpcAddr,
		"--blob", "5",
		"--seed", "1234",
		"--upgrade-schedule", "10:3",
	})
	err := cmd.ExecuteContext(ctx)
	require.NoError(t, err)
}

func TestTxsimCommandEnvVar(t *testing.T) {
	_, _, grpcAddr := setup(t)
	cmd := command()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	os.Setenv(TxsimMnemonic, testfactory.TestAccMnemo)
	os.Setenv(TxsimGRPC, grpcAddr)
	os.Setenv(TxsimSeed, "1234")
	defer os.Clearenv()
	cmd.SetArgs([]string{
		"--blob", "5",
	})
	err := cmd.ExecuteContext(ctx)
	require.NoError(t, err)
}

func TestTxsimDefaultKeypath(t *testing.T) {
	_, _, grpcAddr := setup(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	t.Log("start test")
	t.Log(grpcAddr)

	cmd := command()

	cmd.SetArgs([]string{
		"--blob", "1",
		"--grpc-endpoint", grpcAddr,
		"--seed", "1223",
		"--poll-time", "1s",
		"--feegrant",
	})

	err := cmd.ExecuteContext(ctx)
	require.NoError(t, err)
}

func setup(t testing.TB) (keyring.Keyring, string, string) {
	if testing.Short() {
		t.Skip("skipping tx sim in short mode.")
	}
	t.Helper()

	cdc := encoding.MakeConfig(app.ModuleEncodingRegisters...).Codec

	// set the consensus params to allow for the max square size
	cparams := testnode.DefaultConsensusParams()
	cparams.Block.MaxBytes = int64(appconsts.DefaultMaxBytes)

	cfg := testnode.DefaultConfig().
		WithConsensusParams(cparams).
		WithFundedAccounts(testfactory.TestAccName).
		WithModifiers(
			genesis.FundAccounts(cdc, []sdk.AccAddress{testnode.TestAddress()}, sdk.NewCoin(app.BondDenom, sdk.NewIntFromUint64(1e15))),
		)

	cctx, rpcAddr, grpcAddr := testnode.NewNetwork(t, cfg)

	return cctx.Keyring, rpcAddr, grpcAddr
}
