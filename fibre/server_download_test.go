package fibre_test

import (
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app-fibre/v6/fibre"
	"github.com/celestiaorg/celestia-app-fibre/v6/x/fibre/types"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	"github.com/stretchr/testify/require"
)

// TestServerDownloadShard unit tests the [Server.DownloadShard].
func TestServerDownloadShard(t *testing.T) {
	tests := []struct {
		name            string
		storeBlob       bool // whether to store the blob before download
		requestModifier func(*types.DownloadShardRequest, fibre.Commitment)
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
			name:      "InvalidCommitment_WrongLength",
			storeBlob: false,
			requestModifier: func(req *types.DownloadShardRequest, _ fibre.Commitment) {
				req.Commitment = []byte{1, 2, 3} // too short
			},
			check: func(t *testing.T, resp *types.DownloadShardResponse, err error) {
				require.Error(t, err)
				require.Contains(t, err.Error(), "invalid commitment")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, _, _ := makeTestServer(t)

			// create a test blob for commitment
			blob := makeTestBlobV0(t, 256)
			commitment := blob.Commitment()

			// optionally store the blob
			if tt.storeBlob {
				storeTestShard(t, server, blob, commitment)
			}

			// create download request
			req := &types.DownloadShardRequest{
				Commitment: commitment[:],
			}

			// apply request modifier
			if tt.requestModifier != nil {
				tt.requestModifier(req, commitment)
			}

			resp, err := server.DownloadShard(t.Context(), req)
			tt.check(t, resp, err)
		})
	}
}

// storeTestShard stores a test blob shard in the server's store for download testing.
func storeTestShard(t *testing.T, server *fibre.Server, blob *fibre.Blob, commitment fibre.Commitment) {
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
		BlobVersion:       0,
		Commitment:        commitment,
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

	shard := &types.BlobShard{
		Rows: rows,
		Rlc:  &types.BlobShard_Root{Root: make([]byte, 32)},
	}

	err = server.Store().Put(t.Context(), promise, shard, promise.CreationTimestamp.Add(time.Second))
	require.NoError(t, err)
}
