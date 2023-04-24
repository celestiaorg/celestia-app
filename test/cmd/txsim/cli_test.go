package main

import (
	"context"
	"fmt"
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
	genesis, keyring, err := testnode.DefaultGenesisState(testfactory.TestAccName)
	require.NoError(t, err)

	tmCfg := testnode.DefaultTendermintConfig()
	tmCfg.RPC.ListenAddress = fmt.Sprintf("tcp://127.0.0.1:%d", testnode.GetFreePort())
	tmCfg.P2P.ListenAddress = fmt.Sprintf("tcp://127.0.0.1:%d", testnode.GetFreePort())
	tmCfg.RPC.GRPCListenAddress = fmt.Sprintf("tcp://127.0.0.1:%d", testnode.GetFreePort())

	node, app, cctx, err := testnode.New(
		t,
		testnode.DefaultParams(),
		tmCfg,
		true,
		genesis,
		keyring,
		"testnet",
	)
	require.NoError(t, err)

	cctx, stopNode, err := testnode.StartNode(node, cctx)
	require.NoError(t, err)

	appConf := testnode.DefaultAppConfig()
	appConf.GRPC.Address = fmt.Sprintf("127.0.0.1:%d", testnode.GetFreePort())
	appConf.API.Address = fmt.Sprintf("tcp://127.0.0.1:%d", testnode.GetFreePort())

	_, cleanupGRPC, err := testnode.StartGRPCServer(app, appConf, cctx)
	require.NoError(t, err)

	t.Cleanup(func() {
		t.Log("tearing down testnode")
		require.NoError(t, stopNode())
		require.NoError(t, cleanupGRPC())
	})

	return keyring, tmCfg.RPC.ListenAddress, appConf.GRPC.Address
}
