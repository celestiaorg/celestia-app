package fibre_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/celestiaorg/celestia-app-fibre/v6/app"
	"github.com/celestiaorg/celestia-app-fibre/v6/app/encoding"
	"github.com/celestiaorg/celestia-app-fibre/v6/fibre"
	"github.com/celestiaorg/celestia-app-fibre/v6/fibre/grpc"
	"github.com/celestiaorg/celestia-app-fibre/v6/fibre/validator"
	"github.com/celestiaorg/celestia-app-fibre/v6/x/fibre/types"
	"github.com/celestiaorg/go-square/v4/share"
	"github.com/celestiaorg/rsema1d"
	cmted25519 "github.com/cometbft/cometbft/crypto/ed25519"
	cmtmath "github.com/cometbft/cometbft/libs/math"
	core "github.com/cometbft/cometbft/types"
	"github.com/cosmos/cosmos-sdk/crypto/hd"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"github.com/stretchr/testify/require"
	grpclib "google.golang.org/grpc"
)

const testNamespace = "testns"

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
		{"InsufficientSignaturesCount", testClientUploadInsufficientSignaturesCount},
		{"AllValidatorsReceiveData", testClientUploadAllValidatorsReceiveData},
		{"ClosedClient", testClientUploadClosedClient},
	}

	for _, tt := range tests {
		t.Run(tt.name, tt.fn)
	}
}

func TestNewClient_KeyNotFound(t *testing.T) {
	validators, _ := makeTestValidators(t, 10)
	valSet := validator.Set{ValidatorSet: core.NewValidatorSet(validators), Height: 100}

	// Create empty keyring (no keys)
	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	emptyKeyring := keyring.NewInMemory(encCfg.Codec)

	cfg := fibre.DefaultClientConfig()

	// Attempt to create client with non-existent key
	_, err := fibre.NewClient(nil, emptyKeyring, &mockValidatorSetGetter{set: valSet}, &mockHostRegistry{}, cfg)
	require.Error(t, err)
	require.ErrorIs(t, err, fibre.ErrKeyNotFound, "expected ErrKeyNotFound when key doesn't exist")
	require.Contains(t, err.Error(), cfg.DefaultKeyName, "error should mention the key name")
}

func testClientConcurrentUploads(t *testing.T) {
	client := makeTestClient(t, 100, nil)
	defer client.Close()

	const numConcurrent = 5
	ns := share.MustNewV0Namespace([]byte(testNamespace))

	var wg sync.WaitGroup
	commitments := make(chan rsema1d.Commitment, numConcurrent)

	for range numConcurrent {
		wg.Add(1)
		go func() {
			defer wg.Done()

			blob := makeTestBlobV0(t, 256*1024)
			result, err := client.Upload(t.Context(), ns, blob)
			require.NoError(t, err)

			commitments <- rsema1d.Commitment(result.Commitment)
		}()
	}

	wg.Wait()
	close(commitments)
	require.Len(t, commitments, numConcurrent)
}

func testClientUploadContextCancellation(t *testing.T) {
	client := makeTestClient(t, 100, nil)
	defer client.Close()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	ns := share.MustNewV0Namespace([]byte(testNamespace))
	blob := makeTestBlobV0(t, 1024*1024)

	_, err := client.Upload(ctx, ns, blob)
	require.ErrorIs(t, err, context.Canceled)
}

func testClientUploadSucceedsWithOneThirdFailures(t *testing.T) {
	const numValidators = 100
	client := makeTestClientWithFailures(t, numValidators, 33, nil) // Fail 1/3 of validators
	defer client.Close()

	ns := share.MustNewV0Namespace([]byte(testNamespace))
	blob := makeTestBlobV0(t, 256*1024)

	result, err := client.Upload(t.Context(), ns, blob)
	require.NoError(t, err)
	require.NotEmpty(t, result.ValidatorSignatures)
	require.GreaterOrEqual(t, len(result.ValidatorSignatures), 67, "should have at least 2/3 signatures")
}

func testClientUploadSucceedsWithOneThirdFailuresHighConcurrency(t *testing.T) {
	const numValidators = 100
	// Set concurrency >= validators to test code path where semaphore doesn't limit
	client := makeTestClientWithFailures(t, numValidators, 33, func(cfg *fibre.ClientConfig) {
		cfg.UploadConcurrency = numValidators
	})
	defer client.Close()

	ns := share.MustNewV0Namespace([]byte(testNamespace))
	blob := makeTestBlobV0(t, 256*1024)

	result, err := client.Upload(t.Context(), ns, blob)
	require.NoError(t, err)
	require.NotEmpty(t, result.ValidatorSignatures)
	require.GreaterOrEqual(t, len(result.ValidatorSignatures), 67, "should have at least 2/3 signatures")
}

func testClientUploadInsufficientVotingPower(t *testing.T) {
	const numValidators = 100
	client := makeTestClientWithFailures(t, numValidators, 34, nil) // Fail 1/3+1 validators (34/100)
	defer client.Close()

	ns := share.MustNewV0Namespace([]byte(testNamespace))
	blob := makeTestBlobV0(t, 512*1024)

	_, err := client.Upload(t.Context(), ns, blob)
	require.Error(t, err)

	var notEnoughSigsErr *validator.NotEnoughSignaturesError
	require.ErrorAs(t, err, &notEnoughSigsErr, "expected NotEnoughSignaturesError")
	require.Less(t, notEnoughSigsErr.CollectedPower, notEnoughSigsErr.RequiredPower, "collected power should be less than required")
}

func testClientUploadInsufficientSignaturesCount(t *testing.T) {
	const numValidators = 100
	// Fail 35 validators to get 65 signatures (< 66 required for 2/3)
	// Set low voting power threshold (1/3) so power passes, but high signature count threshold (2/3) so count fails
	client := makeTestClientWithFailures(t, numValidators, 35, func(cfg *fibre.ClientConfig) {
		cfg.SafetyThreshold = cmtmath.Fraction{Numerator: 1, Denominator: 3}             // Low threshold (34 signatures) - will pass
		cfg.UploadTargetSignaturesCount = cmtmath.Fraction{Numerator: 2, Denominator: 3} // High threshold (66 signatures) - will fail
	})
	defer client.Close()

	ns := share.MustNewV0Namespace([]byte(testNamespace))
	blob := makeTestBlobV0(t, 512*1024)

	_, err := client.Upload(t.Context(), ns, blob)
	require.Error(t, err)

	var notEnoughSigsErr *validator.NotEnoughSignaturesError
	require.ErrorAs(t, err, &notEnoughSigsErr, "expected NotEnoughSignaturesError")
	require.Less(t, len(notEnoughSigsErr.Collected), notEnoughSigsErr.RequiredCount, "collected signatures should be less than required")
	require.GreaterOrEqual(t, notEnoughSigsErr.CollectedPower, notEnoughSigsErr.RequiredPower, "collected power should be sufficient")
}

func testClientUploadAllValidatorsReceiveData(t *testing.T) {
	const numValidators = 100
	validators, privKeys := makeTestValidators(t, numValidators)

	tracker := &uploadTracker{uploads: make(map[string]bool)}
	mockClientFn := makeMockClientFn(validators, privKeys, tracker)

	cfg := fibre.DefaultClientConfig()
	cfg.NewClientFn = mockClientFn

	valSet := validator.Set{ValidatorSet: core.NewValidatorSet(validators), Height: 100}
	client, err := fibre.NewClient(nil, makeTestKeyring(t), &mockValidatorSetGetter{set: valSet}, &mockHostRegistry{}, cfg)
	require.NoError(t, err)

	ns := share.MustNewV0Namespace([]byte(testNamespace))
	blob := makeTestBlobV0(t, 256*1024)

	_, err = client.Upload(t.Context(), ns, blob)
	require.NoError(t, err)

	// Close waits for all background upload goroutines to complete
	require.NoError(t, client.Close())

	// Verify all validators received data
	require.Equal(t, numValidators, tracker.uploadCount(), "not all validators received data")
	for _, val := range validators {
		require.True(t, tracker.hasUpload(val.Address.String()), "validator %s did not receive data", val.Address)
	}
}

func makeTestBlobV0(t *testing.T, sizeBytes int) *fibre.Blob {
	t.Helper()
	data := make([]byte, sizeBytes)
	_, err := rand.Read(data)
	require.NoError(t, err)

	blob, err := fibre.NewBlob(data, fibre.DefaultBlobConfigV0())
	require.NoError(t, err)
	return blob
}

func makeTestClient(t *testing.T, numValidators int, customCfg func(*fibre.ClientConfig)) *fibre.Client {
	t.Helper()
	validators, privKeys := makeTestValidators(t, numValidators)
	mockClientFn := makeMockClientFn(validators, privKeys, nil)

	cfg := fibre.DefaultClientConfig()
	cfg.NewClientFn = mockClientFn
	if customCfg != nil {
		customCfg(&cfg)
	}

	valSet := validator.Set{ValidatorSet: core.NewValidatorSet(validators), Height: 100}
	client, err := fibre.NewClient(nil, makeTestKeyring(t), &mockValidatorSetGetter{set: valSet}, &mockHostRegistry{}, cfg)
	require.NoError(t, err)
	return client
}

func makeTestClientWithFailures(t *testing.T, numValidators, numFailures int, customCfg func(*fibre.ClientConfig)) *fibre.Client {
	t.Helper()
	validators, privKeys := makeTestValidators(t, numValidators)

	var failCount atomic.Int32
	mockClientFn := func(ctx context.Context, val *core.Validator) (grpc.Client, error) {
		client, err := makeMockClientFn(validators, privKeys, nil)(ctx, val)
		if err != nil {
			return nil, err
		}

		currentCount := failCount.Add(1)
		shouldFail := currentCount <= int32(numFailures)

		if shouldFail {
			return &failingMockClient{client.(*validatorMockClient)}, nil
		}
		return client, nil
	}

	cfg := fibre.DefaultClientConfig()
	cfg.NewClientFn = mockClientFn
	cfg.UploadConcurrency = 10 // Set lower than numValidators to ensure semaphore limits concurrency
	cfg.SafetyThreshold = cmtmath.Fraction{Numerator: 2, Denominator: 3}
	cfg.UploadTargetSignaturesCount = cmtmath.Fraction{Numerator: 0, Denominator: 1}
	if customCfg != nil {
		customCfg(&cfg)
	}

	valSet := validator.Set{ValidatorSet: core.NewValidatorSet(validators), Height: 100}
	client, err := fibre.NewClient(nil, makeTestKeyring(t), &mockValidatorSetGetter{set: valSet}, &mockHostRegistry{}, cfg)
	require.NoError(t, err)
	return client
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

// Mock infrastructure

type mockValidatorSetGetter struct{ set validator.Set }

func (m *mockValidatorSetGetter) Head(ctx context.Context) (validator.Set, error) {
	return m.set, nil
}

func (m *mockValidatorSetGetter) GetByHeight(ctx context.Context, height uint64) (validator.Set, error) {
	return m.set, nil
}

type mockHostRegistry struct{}

func (m *mockHostRegistry) GetHost(ctx context.Context, val *core.Validator) (validator.Host, error) {
	return validator.Host("localhost:9090"), nil
}

func makeMockClientFn(validators []*core.Validator, privKeys []cmted25519.PrivKey, tracker *uploadTracker) grpc.NewClientFn {
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
			tracker:   tracker,
		}, nil
	}
}

type validatorMockClient struct {
	validator *core.Validator
	privKey   cmted25519.PrivKey
	tracker   *uploadTracker
}

func (v *validatorMockClient) UploadShard(ctx context.Context, req *types.UploadShardRequest, opts ...grpclib.CallOption) (*types.UploadShardResponse, error) {
	if v.tracker != nil {
		v.tracker.recordUpload(v.validator.Address.String())
	}

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

type failingMockClient struct {
	*validatorMockClient
}

func (m *failingMockClient) UploadShard(ctx context.Context, req *types.UploadShardRequest, opts ...grpclib.CallOption) (*types.UploadShardResponse, error) {
	return nil, fmt.Errorf("simulated upload failure")
}

type uploadTracker struct {
	mu      sync.Mutex
	uploads map[string]bool
}

func (u *uploadTracker) recordUpload(validatorAddr string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.uploads[validatorAddr] = true
}

func (u *uploadTracker) uploadCount() int {
	u.mu.Lock()
	defer u.mu.Unlock()
	return len(u.uploads)
}

func (u *uploadTracker) hasUpload(validatorAddr string) bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.uploads[validatorAddr]
}

func testClientUploadClosedClient(t *testing.T) {
	client := makeTestClient(t, 100, nil)

	// Close the client
	require.NoError(t, client.Close())

	// Close again - should be idempotent
	require.NoError(t, client.Close())
	require.NoError(t, client.Close())

	ns := share.MustNewV0Namespace([]byte(testNamespace))
	blob := makeTestBlobV0(t, 256*1024)

	// Attempt to upload after closing
	_, err := client.Upload(t.Context(), ns, blob)
	require.ErrorIs(t, err, fibre.ErrClientClosed, "expected ErrClientClosed when uploading to closed client")
}
