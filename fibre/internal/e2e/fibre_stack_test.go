package e2e_test

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v10/app/encoding"
	"github.com/celestiaorg/celestia-app/v10/fibre"
	grpcfibre "github.com/celestiaorg/celestia-app/v10/fibre/internal/grpc"
	"github.com/celestiaorg/celestia-app/v10/pkg/user"
	"github.com/celestiaorg/celestia-app/v10/test/util/genesis"
	"github.com/celestiaorg/celestia-app/v10/test/util/testnode"
	fibretypes "github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	"github.com/cometbft/cometbft/privval"
	core "github.com/cometbft/cometbft/types"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

// fibreStack is a running FSP server plus a fibre client wired to it and a
// TxClient for the funded default account.
type fibreStack struct {
	server *fibre.Server
	client *fibre.Client
	tx     *user.TxClient
}

func startFibreStack(t *testing.T, cctx testnode.Context, ecfg encoding.Config, grpcAddr string) fibreStack {
	t.Helper()
	ctx := cctx.GoContext()

	pvKeyFile := filepath.Join(cctx.HomeDir, "config", "priv_validator_key.json")
	pvStateFile := filepath.Join(cctx.HomeDir, "data", "priv_validator_state.json")
	filePV := privval.LoadFilePV(pvKeyFile, pvStateFile)

	serverCfg := fibre.DefaultServerConfig()
	serverCfg.AppGRPCAddress = grpcAddr
	serverCfg.ServerListenAddress = "127.0.0.1:0"
	serverCfg.SignerFn = func(_ string) (core.PrivValidator, error) { return filePV, nil }
	serverCfg.StoreFn = func(scfg fibre.StoreConfig) (*fibre.Store, error) {
		return fibre.NewMemoryStore(scfg), nil
	}
	server, err := fibre.NewServer(serverCfg)
	require.NoError(t, err)
	require.NoError(t, server.Start(ctx))

	txClient, err := user.SetupTxClient(ctx, cctx.Keyring, cctx.GRPCClient, ecfg, user.WithDefaultAccount(fibre.DefaultKeyName))
	require.NoError(t, err)

	var client *fibre.Client
	clientCfg := fibre.DefaultClientConfig()
	clientCfg.StateAddress = grpcAddr
	clientCfg.NewClientFn = grpcfibre.DefaultNewClientFn(
		&fixedHostRegistry{addr: server.ListenAddress()},
		func() string { return client.ChainID() },
		clientCfg.MaxMessageSize,
		nil,
	)
	client, err = fibre.NewClient(cctx.Keyring, clientCfg)
	require.NoError(t, err)
	require.NoError(t, client.Start(ctx))

	return fibreStack{server: server, client: client, tx: txClient}
}

// waitForPaymentProcessed waits until the promise is recorded as processed.
// A confirmed settlement tx can precede its state becoming visible to queries
// by a moment, so assertions on settlement state must wait for it.
func waitForPaymentProcessed(t *testing.T, cctx testnode.Context, promiseHash []byte) {
	t.Helper()
	q := fibretypes.NewQueryClient(cctx.GRPCClient)
	require.Eventually(t, func() bool {
		resp, err := q.IsPaymentProcessed(cctx.GoContext(), &fibretypes.QueryIsPaymentProcessedRequest{PromiseHash: promiseHash})
		return err == nil && resp.Found
	}, 30*time.Second, 250*time.Millisecond, "payment promise should be recorded as processed")
}

func fundEscrow(t *testing.T, cctx testnode.Context, tx *user.TxClient, amount sdk.Coin) {
	t.Helper()
	resp, err := tx.SubmitTx(cctx.GoContext(),
		[]sdk.Msg{&fibretypes.MsgDepositToEscrow{Signer: tx.DefaultAddress().String(), Amount: amount}},
		user.SetGasLimit(200_000), user.SetFee(5_000))
	require.NoError(t, err)
	require.Equal(t, uint32(0), resp.Code)
}

func setFibreShortLifetimes(cdc codec.Codec, shardRetention, promiseTimeout time.Duration) genesis.Modifier {
	return func(state map[string]json.RawMessage) map[string]json.RawMessage {
		gs := fibretypes.DefaultGenesis()
		gs.Params.ShardRetention = shardRetention
		gs.Params.PaymentPromiseTimeout = promiseTimeout
		state[fibretypes.ModuleName] = cdc.MustMarshalJSON(gs)
		return state
	}
}
