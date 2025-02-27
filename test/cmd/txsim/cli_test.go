package main

import (
	"context"
	"os"
	"testing"
	"time"

	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/celestiaorg/celestia-app/v4/app"
	"github.com/celestiaorg/celestia-app/v4/app/encoding"
	"github.com/celestiaorg/celestia-app/v4/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v4/test/util/genesis"
	"github.com/celestiaorg/celestia-app/v4/test/util/testfactory"
	"github.com/celestiaorg/celestia-app/v4/test/util/testnode"
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

func setup(t testing.TB) (keyring.Keyring, string, string) {
	if testing.Short() {
		t.Skip("skipping tx sim in short mode.")
	}
	t.Helper()

	enc := encoding.MakeTestConfig(app.ModuleEncodingRegisters...)

	// set the consensus params to allow for the max square size
	cparams := testnode.DefaultConsensusParams()
	cparams.Block.MaxBytes = int64(appconsts.DefaultMaxBytes)

	cfg := testnode.DefaultConfig().
		WithConsensusParams(cparams).
		WithFundedAccounts(testfactory.TestAccName).
		WithModifiers(
			genesis.FundAccounts(enc.Codec, []sdk.AccAddress{testnode.TestAddress()}, sdk.NewCoin(app.BondDenom, math.NewIntFromUint64(1e15))),
		)

	cctx, rpcAddr, grpcAddr := testnode.NewNetwork(t, cfg)

	return cctx.Keyring, rpcAddr, grpcAddr
}
