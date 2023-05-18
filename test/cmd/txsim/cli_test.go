package main

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/test/util/testfactory"
	"github.com/celestiaorg/celestia-app/test/util/testnode"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"github.com/stretchr/testify/require"
)

func TestTxsimCommandFlags(t *testing.T) {
	_, rpcAddr, grpcAddr := setup(t)
	cmd := command()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd.SetArgs([]string{
		"--key-mnemonic", testfactory.TestAccMnemo,
		"--rpc-endpoints", rpcAddr,
		"--grpc-endpoints", grpcAddr,
		"--blob", "5",
		"--seed", "1234",
	})
	err := cmd.ExecuteContext(ctx)
	require.NoError(t, err)
}

func TestTxsimCommandEnvVar(t *testing.T) {
	_, rpcAddr, grpcAddr := setup(t)
	cmd := command()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	os.Setenv(TxsimMnemonic, testfactory.TestAccMnemo)
	os.Setenv(TxsimRPC, rpcAddr)
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
	t.Helper()

	cctx, rpcAddr, grpcAddr := testnode.NewNetwork(
		t,
		testnode.DefaultParams(),
		testnode.DefaultTendermintConfig(),
		testnode.DefaultAppConfig(),
		testfactory.TestAccName,
	)

	return cctx.Keyring, rpcAddr, grpcAddr
}
