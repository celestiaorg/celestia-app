package fibre_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v10/app"
	"github.com/celestiaorg/celestia-app/v10/app/encoding"
	"github.com/celestiaorg/celestia-app/v10/app/grpc/gasestimation"
	"github.com/celestiaorg/celestia-app/v10/app/grpc/tx"
	"github.com/celestiaorg/celestia-app/v10/fibre"
	"github.com/celestiaorg/celestia-app/v10/fibre/state"
	"github.com/celestiaorg/celestia-app/v10/fibre/validator"
	"github.com/celestiaorg/celestia-app/v10/pkg/user"
	"github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	"github.com/cometbft/cometbft/rpc/core"
	coretypes "github.com/cometbft/cometbft/types"
	"github.com/cosmos/cosmos-sdk/crypto/hd"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdktx "github.com/cosmos/cosmos-sdk/types/tx"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

const workerKeyName = "parallel-worker-1"

// TestPutWithTxWorkers runs two concurrent Puts through a two-worker tx client.
// The settlement server only confirms a transaction once a second one is awaiting
// confirmation, so the test passes only if both worker accounts submit in parallel.
func TestPutWithTxWorkers(t *testing.T) {
	const puts = 2
	client, txClient, settlement := newWorkersPutClient(t)
	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)

	results := make([]fibre.PutResult, puts)
	errs := make([]error, puts)
	var wg sync.WaitGroup
	for i := range puts {
		wg.Go(func() {
			results[i], errs[i] = fibre.Put(t.Context(), client, txClient, testNamespace, []byte("hello fibre"))
		})
	}
	wg.Wait()
	for i := range puts {
		require.NoError(t, errs[i])
		require.EqualValues(t, 7, results[i].Height)
	}

	defaultKey, err := txClient.Signer().Keyring().Key(fibre.DefaultKeyName)
	require.NoError(t, err)
	defaultPubKey, err := defaultKey.GetPubKey()
	require.NoError(t, err)

	txs := settlement.transactions()
	require.Len(t, txs, puts)
	signers := make([]string, 0, len(txs))
	for _, raw := range txs {
		sdkTx, err := encCfg.TxConfig.TxDecoder()(raw)
		require.NoError(t, err)
		require.Len(t, sdkTx.GetMsgs(), 1)
		msg, ok := sdkTx.GetMsgs()[0].(*types.MsgPayForFibre)
		require.True(t, ok)

		txSigners, _, err := encCfg.Codec.GetMsgV1Signers(msg)
		require.NoError(t, err)
		require.Equal(t, sdk.AccAddress(txSigners[0]).String(), msg.Signer)
		require.Equal(t, defaultPubKey.Bytes(), msg.PaymentPromise.SignerPublicKey.Key, "every promise must be signed by the default key")
		signers = append(signers, msg.Signer)
	}
	require.ElementsMatch(t, []string{txClient.DefaultAddress().String(), txClient.TxQueueWorkerAddress(1)}, signers, "both worker accounts must submit")
}

// TestPutWithoutTxQueue checks that Put falls back to submitting with the default
// account when the queue is stopped, and that concurrent Puts still pipeline.
func TestPutWithoutTxQueue(t *testing.T) {
	const puts = 2
	client, txClient, settlement := newWorkersPutClient(t)
	txClient.StopTxQueueForTest()

	errs := make([]error, puts)
	var wg sync.WaitGroup
	for i := range puts {
		wg.Go(func() {
			_, errs[i] = fibre.Put(t.Context(), client, txClient, testNamespace, []byte("hello fibre"))
		})
	}
	wg.Wait()
	for i := range puts {
		require.NoError(t, errs[i])
	}

	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	txs := settlement.transactions()
	require.Len(t, txs, puts)
	for _, raw := range txs {
		sdkTx, err := encCfg.TxConfig.TxDecoder()(raw)
		require.NoError(t, err)
		msg, ok := sdkTx.GetMsgs()[0].(*types.MsgPayForFibre)
		require.True(t, ok)
		require.Equal(t, txClient.DefaultAddress().String(), msg.Signer)
	}
}

func newWorkersPutClient(t *testing.T) (*fibre.Client, *user.TxClient, *pairingSettlementServer) {
	t.Helper()
	kr := makeTestKeyring(t)
	_, _, err := kr.NewMnemonic(workerKeyName, keyring.English, "m/44'/118'/0'/0/1", keyring.DefaultBIP39Passphrase, hd.Secp256k1)
	require.NoError(t, err)

	validators, keys := makeTestValidators(t, 4)
	cfg := fibre.DefaultClientConfig()
	cfg.NewClientFn = makeMockClientFn(validators, keys)
	cfg.StateClientFn = func() (state.Client, error) {
		return &mockStateClient{SetGetter: &mockValidatorSetGetter{set: validator.Set{ValidatorSet: coretypes.NewValidatorSet(validators), Height: 100}}, chainID: "celestia"}, nil
	}
	client, err := fibre.NewClient(kr, cfg)
	require.NoError(t, err)
	require.NoError(t, client.Start(t.Context()))
	t.Cleanup(func() { require.NoError(t, client.Stop(context.Background())) })

	settlement := &pairingSettlementServer{}
	server := grpc.NewServer()
	sdktx.RegisterServiceServer(server, settlement)
	tx.RegisterTxServer(server, settlement)
	gasestimation.RegisterGasEstimatorServer(server, settlement)
	listener := bufconn.Listen(1 << 20)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	conn, err := grpc.NewClient("passthrough:///workers-test", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
		return listener.Dial()
	}))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, conn.Close()) })

	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	// Preloading the worker account skips on-chain funding and feegrant setup.
	signer, err := user.NewSigner(kr, encCfg.TxConfig, "celestia",
		user.NewAccount(fibre.DefaultKeyName, 1, 0), user.NewAccount(workerKeyName, 2, 0))
	require.NoError(t, err)
	txClient, err := user.NewTxClient(encCfg.Codec, signer, conn, encCfg.InterfaceRegistry,
		user.WithTxWorkers(2), user.WithPollTime(10*time.Millisecond))
	require.NoError(t, err)
	require.NoError(t, txClient.StartTxQueueForTest(t.Context()))
	t.Cleanup(txClient.StopTxQueueForTest)
	return client, txClient, settlement
}

// pairingSettlementServer records broadcast transactions and reports them as
// committed only once two distinct transactions are awaiting confirmation.
type pairingSettlementServer struct {
	sdktx.UnimplementedServiceServer
	tx.UnimplementedTxServer
	gasestimation.UnimplementedGasEstimatorServer
	mu   sync.Mutex
	txs  [][]byte
	seen map[string]struct{} // tx hashes polled at TxStatus
}

func (s *pairingSettlementServer) EstimateGasPriceAndUsage(context.Context, *gasestimation.EstimateGasPriceAndUsageRequest) (*gasestimation.EstimateGasPriceAndUsageResponse, error) {
	return &gasestimation.EstimateGasPriceAndUsageResponse{EstimatedGasPrice: 0.002, EstimatedGasUsed: 70000}, nil
}

func (s *pairingSettlementServer) BroadcastTx(_ context.Context, req *sdktx.BroadcastTxRequest) (*sdktx.BroadcastTxResponse, error) {
	s.mu.Lock()
	s.txs = append(s.txs, req.TxBytes)
	s.mu.Unlock()
	return &sdktx.BroadcastTxResponse{TxResponse: &sdk.TxResponse{TxHash: txHash(req.TxBytes)}}, nil
}

func (s *pairingSettlementServer) TxStatus(_ context.Context, req *tx.TxStatusRequest) (*tx.TxStatusResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.seen == nil {
		s.seen = make(map[string]struct{})
	}
	s.seen[req.TxId] = struct{}{}
	if len(s.seen) < 2 {
		return &tx.TxStatusResponse{Status: core.TxStatusPending}, nil
	}
	return &tx.TxStatusResponse{Status: core.TxStatusCommitted, Height: 7}, nil
}

func (s *pairingSettlementServer) transactions() [][]byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([][]byte(nil), s.txs...)
}

func txHash(txBytes []byte) string {
	sum := sha256.Sum256(txBytes)
	return hex.EncodeToString(sum[:])
}
