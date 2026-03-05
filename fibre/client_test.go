package fibre_test

import (
	"context"
	"crypto/rand"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/celestiaorg/celestia-app/v8/app"
	"github.com/celestiaorg/celestia-app/v8/app/encoding"
	"github.com/celestiaorg/celestia-app/v8/fibre"
	"github.com/celestiaorg/celestia-app/v8/fibre/internal/grpc"
	"github.com/celestiaorg/celestia-app/v8/fibre/state"
	"github.com/celestiaorg/celestia-app/v8/fibre/validator"
	"github.com/celestiaorg/celestia-app/v8/x/fibre/types"
	"github.com/celestiaorg/go-square/v4/share"
	cmted25519 "github.com/cometbft/cometbft/crypto/ed25519"
	core "github.com/cometbft/cometbft/types"
	"github.com/cosmos/cosmos-sdk/crypto/hd"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"github.com/stretchr/testify/require"
	grpclib "google.golang.org/grpc"
)

func TestNewClient_KeyNotFound(t *testing.T) {
	validators, _ := makeTestValidators(t, 10)
	valSet := validator.Set{ValidatorSet: core.NewValidatorSet(validators), Height: 100}

	// Create empty keyring (no keys)
	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	emptyKeyring := keyring.NewInMemory(encCfg.Codec)

	cfg := fibre.DefaultClientConfig()
	cfg.StateClientFn = func() (state.Client, error) {
		return &mockStateClient{SetGetter: &mockValidatorSetGetter{set: valSet}}, nil
	}

	// Attempt to create client with non-existent key
	_, err := fibre.NewClient(emptyKeyring, cfg)
	require.Error(t, err)
	require.ErrorIs(t, err, fibre.ErrKeyNotFound, "expected ErrKeyNotFound when key doesn't exist")
	require.Contains(t, err.Error(), cfg.DefaultKeyName, "error should mention the key name")
}

var testNamespace = share.MustNewV0Namespace([]byte("test"))

func makeTestBlobV0(t *testing.T, sizeBytes int) *fibre.Blob {
	t.Helper()
	data := make([]byte, sizeBytes)
	_, err := rand.Read(data)
	require.NoError(t, err)

	blob, err := fibre.NewBlob(data, fibre.DefaultBlobConfigV0())
	require.NoError(t, err)
	return blob
}

func makeTestValidators(t *testing.T, n int) ([]*core.Validator, []cmted25519.PrivKey) {
	t.Helper()
	validators := make([]*core.Validator, n)
	privKeys := make([]cmted25519.PrivKey, n)
	for i := range n {
		privKey := cmted25519.GenPrivKey()
		privKeys[i] = privKey
		validators[i] = &core.Validator{
			Address:     privKey.PubKey().Address(),
			PubKey:      privKey.PubKey(),
			VotingPower: 100,
		}
	}
	return validators, privKeys
}

func makeTestKeyring(t *testing.T) keyring.Keyring {
	t.Helper()
	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	kr := keyring.NewInMemory(encCfg.Codec)
	_, _, err := kr.NewMnemonic(fibre.DefaultKeyName, keyring.English, "m/44'/118'/0'/0/0", keyring.DefaultBIP39Passphrase, hd.Secp256k1)
	require.NoError(t, err)
	return kr
}

type mockValidatorSetGetter struct{ set validator.Set }

func (m *mockValidatorSetGetter) Head(ctx context.Context) (validator.Set, error) {
	return m.set, nil
}

func (m *mockValidatorSetGetter) GetByHeight(ctx context.Context, height uint64) (validator.Set, error) {
	return m.set, nil
}

// failingClient is a grpc.Client that always fails all operations.
type failingClient struct{}

func failingClientFn(numFailures int, clientFn grpc.NewClientFn) grpc.NewClientFn {
	var count atomic.Int64
	return func(ctx context.Context, val *core.Validator) (grpc.Client, error) {
		currentCount := count.Add(1)
		if currentCount <= int64(numFailures) {
			return failingClient{}, nil
		}

		return clientFn(ctx, val)
	}
}

func (failingClient) UploadShard(ctx context.Context, req *types.UploadShardRequest, opts ...grpclib.CallOption) (*types.UploadShardResponse, error) {
	return nil, fmt.Errorf("simulated failure")
}

func (failingClient) DownloadShard(ctx context.Context, req *types.DownloadShardRequest, opts ...grpclib.CallOption) (*types.DownloadShardResponse, error) {
	return nil, fmt.Errorf("simulated failure")
}

func (failingClient) Close() error {
	return nil
}

// countingClient wraps a grpc.Client and counts successful downloads.
type countingClient struct {
	client grpc.Client
	count  *atomic.Int64
}

func countingClientFn(clientFn grpc.NewClientFn) (grpc.NewClientFn, *atomic.Int64) {
	var count atomic.Int64
	return func(ctx context.Context, val *core.Validator) (grpc.Client, error) {
		client, err := clientFn(ctx, val)
		if err != nil {
			return nil, err
		}
		return &countingClient{
			client: client,
			count:  &count,
		}, nil
	}, &count
}

func (c *countingClient) UploadShard(ctx context.Context, req *types.UploadShardRequest, opts ...grpclib.CallOption) (*types.UploadShardResponse, error) {
	resp, err := c.client.UploadShard(ctx, req, opts...)
	if err == nil && resp.ValidatorSignature != nil {
		c.count.Add(1)
	}
	return resp, err
}

func (c *countingClient) DownloadShard(ctx context.Context, req *types.DownloadShardRequest, opts ...grpclib.CallOption) (*types.DownloadShardResponse, error) {
	resp, err := c.client.DownloadShard(ctx, req, opts...)
	if err == nil && resp.Shard != nil && len(resp.Shard.Rows) > 0 {
		c.count.Add(1)
	}
	return resp, err
}

func (c *countingClient) Close() error {
	return c.client.Close()
}
