package fibre_test

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/celestiaorg/celestia-app-fibre/v6/fibre"
	"github.com/celestiaorg/celestia-app-fibre/v6/fibre/grpc"
	"github.com/celestiaorg/celestia-app-fibre/v6/fibre/validator"
	"github.com/celestiaorg/celestia-app-fibre/v6/x/fibre/types"
	"github.com/celestiaorg/rsema1d"
	cmted25519 "github.com/cometbft/cometbft/crypto/ed25519"
	core "github.com/cometbft/cometbft/types"
	"github.com/stretchr/testify/require"
	grpclib "google.golang.org/grpc"
)

func TestClientUpload(t *testing.T) {
	tests := []struct {
		name string
		fn   func(*testing.T)
	}{
		{"Concurrent", testClientConcurrentUploads},
		{"ContextCancellation", testClientUploadContextCancellation},
		{"SucceedsWith1/3Failures", testClientUploadSucceedsWithOneThirdFailures},
		{"SucceedsWith1/3Failures_HighConcurrency", testClientUploadSucceedsWithOneThirdFailuresHighConcurrency},
		{"InsufficientVotingPower", testClientUploadInsufficientVotingPower},
		{"AllValidatorsReceiveData", testClientUploadAllValidatorsReceiveData},
		{"ClosedClient", testClientUploadClosedClient},
	}

	for _, tt := range tests {
		t.Run(tt.name, tt.fn)
	}
}

func testClientConcurrentUploads(t *testing.T) {
	client := makeTestUploadClient(t, 100, nil)
	defer client.Close()

	const numConcurrent = 5

	var wg sync.WaitGroup
	commitments := make(chan rsema1d.Commitment, numConcurrent)

	for range numConcurrent {
		wg.Add(1)
		go func() {
			defer wg.Done()

			blob := makeTestBlobV0(t, 256*1024)
			result, err := client.Upload(t.Context(), testNamespace, blob)
			require.NoError(t, err)

			commitments <- result.Commitment
		}()
	}

	wg.Wait()
	close(commitments)
	require.Len(t, commitments, numConcurrent)
}

func testClientUploadContextCancellation(t *testing.T) {
	client := makeTestUploadClient(t, 100, nil)
	defer client.Close()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	blob := makeTestBlobV0(t, 1024*1024)

	_, err := client.Upload(ctx, testNamespace, blob)
	require.ErrorIs(t, err, context.Canceled)
}

func testClientUploadSucceedsWithOneThirdFailures(t *testing.T) {
	const numValidators = 100
	client := makeTestUploadClient(t, numValidators, func(cfg *fibre.ClientConfig) {
		cfg.NewClientFn = failingClientFn(33, cfg.NewClientFn) // Fail 1/3 of validators
	})
	defer client.Close()

	blob := makeTestBlobV0(t, 256*1024)

	result, err := client.Upload(t.Context(), testNamespace, blob)
	require.NoError(t, err)
	require.NotEmpty(t, result.ValidatorSignatures)
	require.GreaterOrEqual(t, len(result.ValidatorSignatures), 67, "should have at least 2/3 signatures")
}

func testClientUploadSucceedsWithOneThirdFailuresHighConcurrency(t *testing.T) {
	const numValidators = 100
	client := makeTestUploadClient(t, numValidators, func(cfg *fibre.ClientConfig) {
		cfg.NewClientFn = failingClientFn(33, cfg.NewClientFn) // Fail 1/3 of validators

		cfg.UploadConcurrency = numValidators // set concurrency >= validators to test code path where semaphore doesn't limit
	})
	defer client.Close()

	blob := makeTestBlobV0(t, 256*1024)

	result, err := client.Upload(t.Context(), testNamespace, blob)
	require.NoError(t, err)
	require.NotEmpty(t, result.ValidatorSignatures)
	require.GreaterOrEqual(t, len(result.ValidatorSignatures), 67, "should have at least 2/3 signatures")
}

func testClientUploadInsufficientVotingPower(t *testing.T) {
	const numValidators = 100
	client := makeTestUploadClient(t, numValidators, func(cfg *fibre.ClientConfig) {
		cfg.NewClientFn = failingClientFn(34, cfg.NewClientFn) // Fail 1/3+1 validators (34/100)
	})
	defer client.Close()

	blob := makeTestBlobV0(t, 512*1024)

	_, err := client.Upload(t.Context(), testNamespace, blob)
	require.Error(t, err)

	var notEnoughSigsErr *validator.NotEnoughSignaturesError
	require.ErrorAs(t, err, &notEnoughSigsErr, "expected NotEnoughSignaturesError")
	require.Less(t, notEnoughSigsErr.CollectedPower, notEnoughSigsErr.RequiredPower, "collected power should be less than required")
}

func testClientUploadAllValidatorsReceiveData(t *testing.T) {
	const numValidators = 100

	var counter *atomic.Int64
	client := makeTestUploadClient(t, numValidators, func(cfg *fibre.ClientConfig) {
		cfg.NewClientFn, counter = countingClientFn(cfg.NewClientFn)
	})
	defer client.Close()

	blob := makeTestBlobV0(t, 256*1024)
	_, err := client.Upload(t.Context(), testNamespace, blob)
	require.NoError(t, err)

	// close waits for all background upload goroutines to complete
	require.NoError(t, client.Close())

	// verify all validators received data
	require.Equal(t, numValidators, int(counter.Load()), "not all validators received data")
}

func testClientUploadClosedClient(t *testing.T) {
	client := makeTestUploadClient(t, 100, nil)

	// close the client
	require.NoError(t, client.Close())
	// close again - should be idempotent
	require.NoError(t, client.Close())

	blob := makeTestBlobV0(t, 256*1024)

	// attempt to upload after closing
	_, err := client.Upload(t.Context(), testNamespace, blob)
	require.ErrorIs(t, err, fibre.ErrClientClosed, "expected ErrClientClosed when uploading to closed client")
}

// makeTestUploadClient creates an upload client for testing.
func makeTestUploadClient(t *testing.T, numValidators int, customCfg func(*fibre.ClientConfig)) *fibre.Client {
	t.Helper()

	cfg := fibre.DefaultClientConfig()
	validators, privKeys := makeTestValidators(t, numValidators)
	cfg.NewClientFn = makeMockClientFn(validators, privKeys)
	if customCfg != nil {
		customCfg(&cfg)
	}

	valSet := validator.Set{ValidatorSet: core.NewValidatorSet(validators), Height: 100}
	client, err := fibre.NewClient(nil, makeTestKeyring(t), &mockValidatorSetGetter{set: valSet}, &mockHostRegistry{}, cfg)
	require.NoError(t, err)
	return client
}

// mock infrastructure

func makeMockClientFn(validators []*core.Validator, privKeys []cmted25519.PrivKey) grpc.NewClientFn {
	privKeyMap := make(map[string]cmted25519.PrivKey)
	for i, val := range validators {
		privKeyMap[val.Address.String()] = privKeys[i]
	}

	return func(ctx context.Context, val *core.Validator) (grpc.Client, error) {
		privKey, ok := privKeyMap[val.Address.String()]
		if !ok {
			return nil, fmt.Errorf("no private key found for validator %s", val.Address)
		}

		return &validatorMockClient{
			validator: val,
			privKey:   privKey,
		}, nil
	}
}

type validatorMockClient struct {
	validator *core.Validator
	privKey   cmted25519.PrivKey
}

func (v *validatorMockClient) UploadShard(ctx context.Context, req *types.UploadShardRequest, opts ...grpclib.CallOption) (*types.UploadShardResponse, error) {
	var pp fibre.PaymentPromise
	if err := pp.FromProto(req.Promise); err != nil {
		return nil, err
	}

	validatorSignBytes, err := pp.SignBytesValidator()
	if err != nil {
		return nil, err
	}

	privKeyBytes := v.privKey.Bytes()
	if len(privKeyBytes) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("invalid private key size: got %d, want %d", len(privKeyBytes), ed25519.PrivateKeySize)
	}

	return &types.UploadShardResponse{
		ValidatorSignature: ed25519.Sign(ed25519.PrivateKey(privKeyBytes), validatorSignBytes),
	}, nil
}

func (v *validatorMockClient) DownloadShard(ctx context.Context, req *types.DownloadShardRequest, opts ...grpclib.CallOption) (*types.DownloadShardResponse, error) {
	return &types.DownloadShardResponse{}, nil
}

func (v *validatorMockClient) Close() error {
	return nil
}
