package fibre_test

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"weak"

	"github.com/celestiaorg/celestia-app/v10/app"
	"github.com/celestiaorg/celestia-app/v10/app/encoding"
	"github.com/celestiaorg/celestia-app/v10/app/grpc/gasestimation"
	"github.com/celestiaorg/celestia-app/v10/app/grpc/tx"
	"github.com/celestiaorg/celestia-app/v10/fibre"
	fibregrpc "github.com/celestiaorg/celestia-app/v10/fibre/internal/grpc"
	"github.com/celestiaorg/celestia-app/v10/fibre/state"
	"github.com/celestiaorg/celestia-app/v10/fibre/validator"
	"github.com/celestiaorg/celestia-app/v10/pkg/user"
	"github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	"github.com/cometbft/cometbft/rpc/core"
	coretypes "github.com/cometbft/cometbft/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdktx "github.com/cosmos/cosmos-sdk/types/tx"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

func TestPutReleasesPayloadAfterLastReaderBeforeConfirmation(t *testing.T) {
	for _, canceled := range []bool{false, true} {
		t.Run(fmt.Sprintf("canceled=%t", canceled), func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			entered, release := make(chan struct{}), make(chan struct{})
			checked := make(chan error, 1)
			var releaseOnce sync.Once
			unblock := func() { releaseOnce.Do(func() { close(release) }) }
			var clients atomic.Int64
			client, txClient, settlement := newLifetimePutClient(t, func(next fibregrpc.NewClientFn) fibregrpc.NewClientFn {
				return func(ctx context.Context, val *coretypes.Validator) (fibregrpc.Client, error) {
					c, err := next(ctx, val)
					if err != nil || clients.Add(1) != 1 {
						return c, err
					}
					return &delayedUploadReader{Client: c, entered: entered, release: release, checked: checked}, nil
				}
			})
			t.Cleanup(unblock)
			type outcome struct {
				result fibre.PutResult
				err    error
			}
			done := make(chan outcome, 1)
			var raw weak.Pointer[byte]
			newData := func() []byte {
				data := bytes.Repeat([]byte{0x47}, 256<<10)
				raw = weak.Make(&data[0])
				return data
			}
			go func() {
				result, err := fibre.Put(ctx, client, txClient, testNamespace, newData())
				done <- outcome{result, err}
			}()
			awaitLifetimeSignal(t, entered)
			awaitLifetimeSignal(t, settlement.entered)

			if canceled {
				cancel()
				select {
				case result := <-done:
					require.Error(t, result.err)
				case <-time.After(10 * time.Second):
					t.Fatal("Put did not stop waiting for confirmation")
				}
			}

			// Reusing the same pool shape must not overwrite the delayed RPC's rows.
			other, err := fibre.NewBlob(bytes.Repeat([]byte{0xa5}, 256<<10), fibre.DefaultBlobConfigV0())
			require.NoError(t, err)
			other.Free()
			unblock()
			require.NoError(t, <-checked)
			client.Await()
			if !canceled {
				require.Eventually(t, func() bool {
					runtime.GC()
					return raw.Value() == nil
				}, 5*time.Second, 10*time.Millisecond, "confirmation retained the original payload")
				select {
				case <-done:
					t.Fatal("Put returned before confirmation")
				default:
				}
				close(settlement.confirm)
				select {
				case result := <-done:
					require.NoError(t, result.err)
					require.EqualValues(t, 123, result.result.Height)
					require.Equal(t, "lifetime-test", result.result.TxHash)
				case <-time.After(10 * time.Second):
					t.Fatal("Put did not return after confirmation")
				}
			}
		})
	}
}

type delayedUploadReader struct {
	fibregrpc.Client
	entered, release chan struct{}
	checked          chan error
}

func (c *delayedUploadReader) UploadShard(ctx context.Context, req *types.UploadShardRequest, opts ...grpc.CallOption) (*types.UploadShardResponse, error) {
	before, err := req.Marshal()
	if err != nil {
		return nil, err
	}
	close(c.entered)
	<-c.release // Model an RPC still reading buffers while cancellation propagates.
	after, err := req.Marshal()
	if err == nil && !bytes.Equal(before, after) {
		err = fmt.Errorf("upload rows or proofs changed before the RPC finished")
	}
	c.checked <- err
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return c.Client.UploadShard(ctx, req, opts...)
}

func newLifetimePutClient(t *testing.T, wrap func(fibregrpc.NewClientFn) fibregrpc.NewClientFn) (*fibre.Client, *user.TxClient, *lifetimeSettlementServer) {
	t.Helper()
	kr := makeTestKeyring(t)
	validators, keys := makeTestValidators(t, 4)
	cfg := fibre.DefaultClientConfig()
	cfg.NewClientFn = makeMockClientFn(validators, keys)
	if wrap != nil {
		cfg.NewClientFn = wrap(cfg.NewClientFn)
	}
	cfg.StateClientFn = func() (state.Client, error) {
		return &mockStateClient{SetGetter: &mockValidatorSetGetter{set: validator.Set{ValidatorSet: coretypes.NewValidatorSet(validators), Height: 100}}, chainID: "celestia"}, nil
	}
	client, err := fibre.NewClient(kr, cfg)
	require.NoError(t, err)
	require.NoError(t, client.Start(t.Context()))
	t.Cleanup(func() { require.NoError(t, client.Stop(context.Background())) })

	settlement := &lifetimeSettlementServer{entered: make(chan struct{}), confirm: make(chan struct{})}
	server := grpc.NewServer()
	sdktx.RegisterServiceServer(server, settlement)
	tx.RegisterTxServer(server, settlement)
	gasestimation.RegisterGasEstimatorServer(server, settlement)
	listener := bufconn.Listen(1 << 20)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	conn, err := grpc.NewClient("passthrough:///lifetime-test", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
		return listener.Dial()
	}))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, conn.Close()) })
	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	signer, err := user.NewSigner(kr, encCfg.TxConfig, "celestia", user.NewAccount(fibre.DefaultKeyName, 1, 0))
	require.NoError(t, err)
	txClient, err := user.NewTxClient(encCfg.Codec, signer, conn, encCfg.InterfaceRegistry)
	require.NoError(t, err)
	return client, txClient, settlement
}

type lifetimeSettlementServer struct {
	sdktx.UnimplementedServiceServer
	tx.UnimplementedTxServer
	gasestimation.UnimplementedGasEstimatorServer
	entered, confirm chan struct{}
}

func (s *lifetimeSettlementServer) EstimateGasPriceAndUsage(context.Context, *gasestimation.EstimateGasPriceAndUsageRequest) (*gasestimation.EstimateGasPriceAndUsageResponse, error) {
	return &gasestimation.EstimateGasPriceAndUsageResponse{EstimatedGasPrice: 0.002, EstimatedGasUsed: 70000}, nil
}

func (s *lifetimeSettlementServer) BroadcastTx(context.Context, *sdktx.BroadcastTxRequest) (*sdktx.BroadcastTxResponse, error) {
	return &sdktx.BroadcastTxResponse{TxResponse: &sdk.TxResponse{TxHash: "lifetime-test"}}, nil
}

func (s *lifetimeSettlementServer) TxStatus(ctx context.Context, _ *tx.TxStatusRequest) (*tx.TxStatusResponse, error) {
	close(s.entered)
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-s.confirm:
		return &tx.TxStatusResponse{Status: core.TxStatusCommitted, Height: 123}, nil
	}
}

func awaitLifetimeSignal(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for upload stage")
	}
}
