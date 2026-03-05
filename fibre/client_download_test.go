package fibre_test

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/celestiaorg/celestia-app/v8/fibre"
	"github.com/celestiaorg/celestia-app/v8/fibre/grpc"
	"github.com/celestiaorg/celestia-app/v8/fibre/validator"
	"github.com/celestiaorg/celestia-app/v8/x/fibre/types"
	cmted25519 "github.com/cometbft/cometbft/crypto/ed25519"
	core "github.com/cometbft/cometbft/types"
	"github.com/stretchr/testify/require"
	grpclib "google.golang.org/grpc"
)

func TestClientDownload(t *testing.T) {
	tests := []struct {
		name string
		fn   func(*testing.T)
	}{
		{"Success", testClientDownloadSuccess},
		{"Success_ExactTargetCount", testClientDownloadExactTargetCount},
		{"Success_Concurrent", testClientDownloadConcurrent},
		{"FaultTolerance", testClientDownloadFaultTolerance},
		{"ContextCancellation", testClientDownloadContextCancellation},
		{"ClosedClient", testClientDownloadClosedClient},
	}

	for _, tt := range tests {
		t.Run(tt.name, tt.fn)
	}
}

func testClientDownloadSuccess(t *testing.T) {
	blob := makeTestBlobV0(t, 256*1024)
	client := makeTestDownloadClient(t, 10, nil, blob)
	defer client.Close()

	downloaded, err := client.Download(t.Context(), blob.ID())
	require.NoError(t, err)
	require.NotNil(t, downloaded)
	require.Equal(t, blob.Data(), downloaded.Data())
}

func testClientDownloadConcurrent(t *testing.T) {
	const numConcurrent = 5

	blobs := make([]*fibre.Blob, numConcurrent)
	for i := range numConcurrent {
		blobs[i] = makeTestBlobV0(t, 256*1024)
	}

	client := makeTestDownloadClient(t, 100, nil, blobs...)
	defer client.Close()

	var wg sync.WaitGroup
	for _, blob := range blobs {
		wg.Add(1)
		go func(blob *fibre.Blob) {
			defer wg.Done()

			downloaded, err := client.Download(t.Context(), blob.ID())
			require.NoError(t, err)
			require.Equal(t, blob.Data(), downloaded.Data())
		}(blob)
	}
	wg.Wait()
}

func testClientDownloadContextCancellation(t *testing.T) {
	blob := makeTestBlobV0(t, 256*1024)
	client := makeTestDownloadClient(t, 10, nil, blob)
	defer client.Close()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := client.Download(ctx, blob.ID())
	require.ErrorIs(t, err, context.Canceled)
}

func testClientDownloadClosedClient(t *testing.T) {
	blob := makeTestBlobV0(t, 256*1024)
	client := makeTestDownloadClient(t, 10, nil, blob)
	defer client.Close()

	require.NoError(t, client.Close())
	require.NoError(t, client.Close()) // idempotent

	_, err := client.Download(t.Context(), blob.ID())
	require.ErrorIs(t, err, fibre.ErrClientClosed)
}

func testClientDownloadExactTargetCount(t *testing.T) {
	// test that we download from exactly downloadTarget validators (no more)
	// with 10 equal-stake validators and livenessThreshold=1/3, downloadTarget = 4
	const numValidators = 10

	blob := makeTestBlobV0(t, 256*1024)

	var counter *atomic.Int64
	client := makeTestDownloadClient(t, numValidators, func(cfg *fibre.ClientConfig) {
		cfg.NewClientFn, counter = countingClientFn(cfg.NewClientFn)
	}, blob)
	defer client.Close()

	downloaded, err := client.Download(t.Context(), blob.ID())
	require.NoError(t, err)
	require.Equal(t, blob.Data(), downloaded.Data())

	// Select returns minRequired = 4 for 10 equal-stake validators with livenessThreshold=1/3
	// we should have exactly 4 successful downloads (no over-fetching in happy path)
	require.Equal(t, int64(4), counter.Load(), "should download from exactly downloadTarget validators")
}

func testClientDownloadFaultTolerance(t *testing.T) {
	// test failure tolerance boundaries with 10 validators
	// Select uses livenessThreshold (1/3), so downloadTarget = 4 for 10 equal-stake validators
	const numValidators = 10
	blob := makeTestBlobV0(t, 256*1024)

	tests := []struct {
		failures  int
		expectErr error
	}{
		{10, fibre.ErrNotFound},
		{7, fibre.ErrNotEnoughShards}, // 3 successes, need 4
		{6, nil},                      // 4 successes, exactly enough
		{5, nil},                      // 5 successes, more than enough
		{4, nil},                      // 6 successes, more than enough
	}

	for _, tc := range tests {
		t.Run(fmt.Sprintf("%d_failures", tc.failures), func(t *testing.T) {
			client := makeTestDownloadClient(t, numValidators, func(cfg *fibre.ClientConfig) {
				cfg.NewClientFn = failingClientFn(tc.failures, cfg.NewClientFn)
			}, blob)
			defer client.Close()

			downloaded, err := client.Download(t.Context(), blob.ID())
			if tc.expectErr != nil {
				require.ErrorIs(t, err, tc.expectErr)
			} else {
				require.NoError(t, err)
				require.Equal(t, blob.Data(), downloaded.Data())
			}
		})
	}
}

// makeTestDownloadClient creates a download client that serves the given blobs.
// numFailures specifies how many validators should fail (0 for none).
func makeTestDownloadClient(
	t *testing.T,
	numValidators int,
	customCfg func(*fibre.ClientConfig),
	blobs ...*fibre.Blob,
) *fibre.Client {
	t.Helper()

	validators, privKeys := makeTestValidators(t, numValidators)
	cfg := fibre.DefaultClientConfig()
	cfg.NewClientFn = makeDownloadMockClientFn(validators, privKeys, blobs...)
	if customCfg != nil {
		customCfg(&cfg)
	}

	valSet := validator.Set{ValidatorSet: core.NewValidatorSet(validators), Height: 100}
	client, err := fibre.NewClient(nil, makeTestKeyring(t), &mockValidatorSetGetter{set: valSet}, &mockHostRegistry{}, cfg)
	require.NoError(t, err)
	return client
}

// makeDownloadMockClientFn creates a mock client function for download tests.
func makeDownloadMockClientFn(
	validators []*core.Validator,
	privKeys []cmted25519.PrivKey,
	blobs ...*fibre.Blob,
) func(context.Context, *core.Validator) (grpc.Client, error) {
	return func(ctx context.Context, val *core.Validator) (grpc.Client, error) {
		valIdx := -1
		for i, v := range validators {
			if v.Address.String() == val.Address.String() {
				valIdx = i
				break
			}
		}

		client := &downloadMockClient{
			validator:     val,
			valIdx:        valIdx,
			numValidators: len(validators),
			privKey:       privKeys[valIdx],
			blobs:         blobs,
		}
		return client, nil
	}
}

// mock infrastructure

type downloadMockClient struct {
	validator     *core.Validator
	valIdx        int
	numValidators int
	privKey       cmted25519.PrivKey
	blobs         []*fibre.Blob
}

func (d *downloadMockClient) UploadShard(ctx context.Context, req *types.UploadShardRequest, opts ...grpclib.CallOption) (*types.UploadShardResponse, error) {
	return &types.UploadShardResponse{}, nil
}

func (d *downloadMockClient) DownloadShard(ctx context.Context, req *types.DownloadShardRequest, opts ...grpclib.CallOption) (*types.DownloadShardResponse, error) {
	var id fibre.BlobID
	if err := id.UnmarshalBinary(req.BlobId); err != nil {
		return nil, err
	}

	// find the blob matching the commitment
	var blob *fibre.Blob
	for _, b := range d.blobs {
		if b.ID().Commitment() == id.Commitment() {
			blob = b
			break
		}
	}
	if blob == nil {
		return &types.DownloadShardResponse{}, nil
	}

	// determine which rows this validator should return
	blobCfg := fibre.DefaultBlobConfigV0()
	totalRows := blobCfg.OriginalRows + blobCfg.ParityRows

	var rowIndices []int
	for i := range totalRows {
		if i%d.numValidators == d.valIdx {
			rowIndices = append(rowIndices, i)
		}
	}

	rows := make([]*types.BlobRow, 0, len(rowIndices))
	var rlcRoot [32]byte
	for _, idx := range rowIndices {
		row, err := blob.Row(idx)
		if err != nil {
			continue
		}
		rows = append(rows, &types.BlobRow{
			Index: uint32(row.Index),
			Data:  row.Row,
			Proof: row.RowProof.RowProof,
		})
		if len(rows) == 1 {
			rlcRoot = row.RLCRoot
		}
	}

	return &types.DownloadShardResponse{
		Shard: &types.BlobShard{
			Rlc:  &types.BlobShard_Root{Root: rlcRoot[:]},
			Rows: rows,
		},
	}, nil
}

func (d *downloadMockClient) Close() error {
	return nil
}
