package fibre_test

import (
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v9/fibre"
	"github.com/celestiaorg/celestia-app/v9/pkg/rsema1d/field"
	"github.com/celestiaorg/celestia-app/v9/x/fibre/types"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	"github.com/stretchr/testify/require"
)

// TestServerDownloadShard unit tests the [Server.DownloadShard].
func TestServerDownloadShard(t *testing.T) {
	tests := []struct {
		name            string
		storeBlob       bool // whether to store the blob before download
		requestModifier func(*types.DownloadShardRequest, fibre.BlobID)
		check           func(*testing.T, *types.DownloadShardResponse, error)
	}{
		{
			name:            "Success",
			storeBlob:       true,
			requestModifier: nil,
			check: func(t *testing.T, resp *types.DownloadShardResponse, err error) {
				require.NoError(t, err)
				require.NotNil(t, resp)
				require.NotNil(t, resp.Shard)
				require.NotEmpty(t, resp.Shard.Rows)
			},
		},
		{
			name:      "NotFound",
			storeBlob: false, // don't store the blob
			check: func(t *testing.T, resp *types.DownloadShardResponse, err error) {
				require.Error(t, err)
				require.Contains(t, err.Error(), "no blob shard found")
			},
		},
		{
			name:      "InvalidBlobID_WrongLength",
			storeBlob: false,
			requestModifier: func(req *types.DownloadShardRequest, _ fibre.BlobID) {
				req.BlobId = []byte{1, 2, 3} // too short
			},
			check: func(t *testing.T, resp *types.DownloadShardResponse, err error) {
				require.Error(t, err)
				require.Contains(t, err.Error(), "invalid blob ID")
			},
		},
		{
			name:      "WithRlc_ReturnsCoefficients",
			storeBlob: true,
			requestModifier: func(req *types.DownloadShardRequest, _ fibre.BlobID) {
				req.WithRlc = true
			},
			check: func(t *testing.T, resp *types.DownloadShardResponse, err error) {
				require.NoError(t, err)
				require.NotNil(t, resp.Shard)
				require.NotEmpty(t, resp.Shard.Rows)
				require.NotEmpty(t, resp.Shard.Coefficients,
					"coefficients should be present when WithRlc is true")
				require.NotEmpty(t, resp.Shard.Root,
					"RLC root should be present")
			},
		},
		{
			name:      "WithoutRlc_StripsCoefficients",
			storeBlob: true,
			check: func(t *testing.T, resp *types.DownloadShardResponse, err error) {
				require.NoError(t, err)
				require.NotNil(t, resp.Shard)
				require.NotEmpty(t, resp.Shard.Rows)
				require.Empty(t, resp.Shard.Coefficients,
					"coefficients should be stripped when WithRlc is false")
				require.NotEmpty(t, resp.Shard.Root,
					"RLC root should always be present")
			},
		},
		{
			name:      "UnsupportedBlobVersion",
			storeBlob: false,
			requestModifier: func(req *types.DownloadShardRequest, id fibre.BlobID) {
				req.BlobId = fibre.NewBlobID(99, id.Commitment()) // unsupported version
			},
			check: func(t *testing.T, resp *types.DownloadShardResponse, err error) {
				require.Error(t, err)
				require.Contains(t, err.Error(), "unsupported blob version")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, _, _ := makeTestServer(t)

			// create a test blob
			blob := makeTestBlobV0(t, 256)
			id := blob.ID()

			// optionally store the blob
			if tt.storeBlob {
				storeTestShard(t, server, blob)
			}

			// create download request
			req := &types.DownloadShardRequest{
				BlobId: id,
			}

			// apply request modifier
			if tt.requestModifier != nil {
				tt.requestModifier(req, id)
			}

			resp, err := server.DownloadShard(t.Context(), req)
			tt.check(t, resp, err)
		})
	}
}

// storeTestShard stores a test blob shard in the server's store for download testing.
func storeTestShard(t *testing.T, server *fibre.Server, blob *fibre.Blob) {
	t.Helper()

	// create a payment promise
	keyring := makeTestKeyring(t)
	key, err := keyring.Key(fibre.DefaultKeyName)
	require.NoError(t, err)
	pubKey, err := key.GetPubKey()
	require.NoError(t, err)

	promise := &fibre.PaymentPromise{
		ChainID:           "celestia",
		Height:            100,
		Namespace:         testNamespace,
		UploadSize:        uint32(blob.UploadSize()),
		BlobVersion:       uint32(blob.ID().Version()),
		Commitment:        blob.ID().Commitment(),
		CreationTimestamp: time.Now(),
		SignerKey:         pubKey.(*secp256k1.PubKey),
		Signature:         []byte("test-signature"),
	}

	// create rows for the shard
	rows := make([]*types.BlobRow, 3)
	for i := range 3 {
		rowProof, err := blob.Row(i)
		require.NoError(t, err)
		rows[i] = &types.BlobRow{
			Index: uint32(i),
			Data:  rowProof.Row,
			Proof: rowProof.RowProof.RowProof,
		}
	}

	// flatten RLC coefficients for storage
	rlcCoeffs := blob.RLC()
	coeffBytes := make([]byte, len(rlcCoeffs)*16)
	for i, c := range rlcCoeffs {
		b := field.ToBytes128(c)
		copy(coeffBytes[i*16:(i+1)*16], b[:])
	}

	shard := &types.BlobShard{
		Rows:         rows,
		Root:         make([]byte, 32),
		Coefficients: coeffBytes,
	}

	err = server.Store().Put(t.Context(), promise, shard, promise.CreationTimestamp.Add(time.Second))
	require.NoError(t, err)
}
