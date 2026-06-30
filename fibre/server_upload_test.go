package fibre_test

import (
	"crypto/ed25519"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v10/fibre"
	"github.com/celestiaorg/celestia-app/v10/fibre/validator"
	"github.com/celestiaorg/celestia-app/v10/pkg/rsema1d"
	"github.com/celestiaorg/celestia-app/v10/pkg/rsema1d/rlc"
	"github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	core "github.com/cometbft/cometbft/types"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	txsigning "github.com/cosmos/cosmos-sdk/types/tx/signing"
	clock "github.com/filecoin-project/go-clock"
	"github.com/stretchr/testify/require"
	grpccodes "google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TestServerUploadShard unit tests the [Server.UploadShard].
// It currently covers random cases and should be eventually extended for 100% coverage.
// The request modifier approach should allow simulating any failure.
func TestServerUploadShard(t *testing.T) {
	server, valSet, serverValidator := makeTestServer(t)

	tests := []struct {
		name            string
		requestModifier func(*types.UploadShardRequest)
		check           func(*testing.T, *types.UploadShardResponse, error)
	}{
		{
			name:            "Success",
			requestModifier: nil,
			check: func(t *testing.T, resp *types.UploadShardResponse, err error) {
				require.NoError(t, err)
				require.NotNil(t, resp)
				require.NotEmpty(t, resp.ValidatorSignature)
				require.Len(t, resp.ValidatorSignature, ed25519.SignatureSize)
			},
		},
		{
			name: "InvalidPaymentPromise",
			requestModifier: func(req *types.UploadShardRequest) {
				// invalidate promise by removing signature
				req.Promise.Signature = nil
			},
			check: func(t *testing.T, resp *types.UploadShardResponse, err error) {
				require.Error(t, err)
				require.Contains(t, err.Error(), "payment promise validation failed")
			},
		},
		{
			name: "WrongChainID",
			requestModifier: func(req *types.UploadShardRequest) {
				// set wrong chain ID
				req.Promise.ChainId = "wrong-chain"
			},
			check: func(t *testing.T, resp *types.UploadShardResponse, err error) {
				require.Error(t, err)
				require.Contains(t, err.Error(), "chain ID mismatch")
			},
		},
		{
			name: "TimestampTooOld",
			requestModifier: func(req *types.UploadShardRequest) {
				// set timestamp 2 hours ago (exceeds default 1 hour PaymentPromiseTimeout)
				req.Promise.CreationTimestamp = time.Now().Add(-2 * time.Hour)
			},
			check: func(t *testing.T, resp *types.UploadShardResponse, err error) {
				require.Error(t, err)
				require.Contains(t, err.Error(), "payment promise expired")
			},
		},
		{
			name: "InvalidRowAssignment",
			requestModifier: func(req *types.UploadShardRequest) {
				// replace with another validator's rows
				serverCfg := server.Config
				blobCfg, _ := fibre.BlobConfigForVersion(uint8(req.Promise.BlobVersion))
				totalRows := blobCfg.OriginalRows + blobCfg.ParityRows
				// get commitment from the request (it's already a byte slice)
				var commitment rsema1d.Commitment
				copy(commitment[:], req.Promise.Commitment)
				shardMap := valSet.Assign(commitment, totalRows, blobCfg.OriginalRows, serverCfg.MinRowsPerValidator, serverCfg.LivenessThreshold)
				for val, indices := range shardMap {
					if val.Address.String() != serverValidator.Address.String() && len(indices) > 0 {
						req.Shard.Rows[0].Index = uint32(indices[0])
						break
					}
				}
			},
			check: func(t *testing.T, resp *types.UploadShardResponse, err error) {
				require.Error(t, err)
				require.Contains(t, err.Error(), "shard assignment verification failed")
			},
		},
		{
			name: "DuplicateRows",
			requestModifier: func(req *types.UploadShardRequest) {
				// repeat the first assigned row across every slot: the count and
				// membership checks pass, but the rows are not unique
				require.Greater(t, len(req.Shard.Rows), 1, "need >1 assigned row to duplicate")
				for i := range req.Shard.Rows {
					req.Shard.Rows[i] = req.Shard.Rows[0]
				}
			},
			check: func(t *testing.T, resp *types.UploadShardResponse, err error) {
				require.Error(t, err)
				require.Contains(t, err.Error(), "shard assignment verification failed")
				require.Contains(t, err.Error(), "duplicate row")
			},
		},
		{
			name: "InvalidRowProof",
			requestModifier: func(req *types.UploadShardRequest) {
				// corrupt the proof
				req.Shard.Rows[0].Proof[0] = []byte("invalid proof")
			},
			check: func(t *testing.T, resp *types.UploadShardResponse, err error) {
				require.Error(t, err)
				require.Contains(t, err.Error(), "verification failed")
			},
		},
		{
			name: "MissingRows",
			requestModifier: func(req *types.UploadShardRequest) {
				// remove all rows
				req.Shard.Rows = nil
			},
			check: func(t *testing.T, resp *types.UploadShardResponse, err error) {
				require.Error(t, err)
			},
		},
		{
			name: "NilShard",
			requestModifier: func(req *types.UploadShardRequest) {
				// omit the entire Shard message (a peer can send UploadShard
				// with the Shard field unset). The handler must reject this
				// gracefully rather than panicking on a nil dereference.
				req.Shard = nil
			},
			check: func(t *testing.T, resp *types.UploadShardResponse, err error) {
				require.Error(t, err)
				require.Nil(t, resp)
			},
		},
		{
			name: "InvalidUploadSize",
			requestModifier: func(req *types.UploadShardRequest) {
				// set wrong upload size
				req.Promise.BlobSize = 12345
			},
			check: func(t *testing.T, resp *types.UploadShardResponse, err error) {
				require.Error(t, err)
				require.Contains(t, err.Error(), "upload size mismatch")
			},
		},
		{
			name: "OversizedRow",
			requestModifier: func(req *types.UploadShardRequest) {
				// craft rows one chunk larger than the protocol maximum. The
				// handler must reject these before they are signed/stored, since
				// the read-side row pool is sized for MaxRowSize.
				blobCfg, _ := fibre.BlobConfigForVersion(uint8(req.Promise.BlobVersion))
				oversized := blobCfg.MaxRowSize + 64
				for i := range req.Shard.Rows {
					req.Shard.Rows[i].Data = make([]byte, oversized)
				}
				req.Promise.BlobSize = uint32(oversized * blobCfg.OriginalRows)
			},
			check: func(t *testing.T, resp *types.UploadShardResponse, err error) {
				require.Error(t, err)
				require.Contains(t, err.Error(), "exceeds maximum")
				require.Nil(t, resp)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := makeTestRequest(t, valSet, serverValidator, tt.requestModifier)
			resp, err := server.UploadShard(t.Context(), req)
			tt.check(t, resp, err)
		})
	}
}

// TestServerUploadShardRateLimit exercises the upload admission controller.
func TestServerUploadShardRateLimit(t *testing.T) {
	// Size the burst to exactly one upload (derived from the blob layout, which
	// rounds the row count up for the blob header) so a full bucket admits the
	// first upload and a drained bucket rejects the next.
	uploadSize := makeTestBlobV0(t, 256*1024).UploadSize()

	t.Run("over-budget verified upload returns ResourceExhausted without storing or signing", func(t *testing.T) {
		server, valSet, serverValidator := makeTestServer(t, func(cfg *fibre.ServerConfig) {
			cfg.Clock = clock.NewMock() // never advanced: budget cannot refill
			cfg.UploadRateLimitBytesPerSecond = 1
			cfg.UploadRateLimitBurstBytes = uploadSize
			cfg.UploadRateLimitMaxWait = "0s"
			cfg.MaxUploadShardInFlight = 8
		})

		// First verified upload drains the full bucket and succeeds.
		req1 := makeTestRequest(t, valSet, serverValidator, nil)
		resp1, err := server.UploadShard(t.Context(), req1)
		require.NoError(t, err)
		require.NotEmpty(t, resp1.ValidatorSignature)

		// Second verified upload needs refill exceeding maxWait (0) → rejected.
		req2 := makeTestRequest(t, valSet, serverValidator, nil)
		resp2, err := server.UploadShard(t.Context(), req2)
		require.Error(t, err)
		require.Nil(t, resp2)
		require.Equal(t, grpccodes.ResourceExhausted, status.Code(err))

		// Rejection happens before store.Put, so nothing was persisted for it.
		var commitment2 fibre.Commitment
		copy(commitment2[:], req2.Promise.Commitment)
		_, getErr := server.Store().Get(t.Context(), commitment2)
		require.ErrorIs(t, getErr, fibre.ErrStoreNotFound)
	})

	t.Run("invalid shard does not consume byte budget", func(t *testing.T) {
		server, valSet, serverValidator := makeTestServer(t, func(cfg *fibre.ServerConfig) {
			cfg.Clock = clock.NewMock()
			cfg.UploadRateLimitBytesPerSecond = 1
			cfg.UploadRateLimitBurstBytes = uploadSize // room for exactly one blob
			cfg.UploadRateLimitMaxWait = "0s"
			cfg.MaxUploadShardInFlight = 8
		})

		// An invalid upload (corrupt proof) is rejected at verification. Because
		// the byte budget is charged only after verification, it must not drain
		// the bucket.
		badReq := makeTestRequest(t, valSet, serverValidator, func(req *types.UploadShardRequest) {
			req.Shard.Rows[0].Proof[0] = []byte("invalid proof")
		})
		_, err := server.UploadShard(t.Context(), badReq)
		require.Error(t, err)
		require.Equal(t, grpccodes.InvalidArgument, status.Code(err))

		// The full burst is still available, so a subsequent valid upload admits.
		goodReq := makeTestRequest(t, valSet, serverValidator, nil)
		resp, err := server.UploadShard(t.Context(), goodReq)
		require.NoError(t, err)
		require.NotEmpty(t, resp.ValidatorSignature)
	})
}

// makeTestRequest creates a valid UploadShardRequest for the given test setup.
// Optional modifier can be provided to customize the request after construction.
// The promise is automatically re-signed after modification.
func makeTestRequest(
	t *testing.T,
	valSet validator.Set,
	serverValidator *core.Validator,
	requestModifier func(*types.UploadShardRequest),
) *types.UploadShardRequest {
	t.Helper()

	blob := makeTestBlobV0(t, 256*1024)

	// create and sign payment promise
	keyring := makeTestKeyring(t)
	key, err := keyring.Key(fibre.DefaultKeyName)
	require.NoError(t, err)
	pubKey, err := key.GetPubKey()
	require.NoError(t, err)

	// sign function that can be called to (re)sign the promise
	signPromise := func(promisePb *types.PaymentPromise) {
		// load into PaymentPromise to compute sign bytes
		promise := &fibre.PaymentPromise{}
		require.NoError(t, promise.FromProto(promisePb))

		signBytes, err := promise.SignBytes()
		require.NoError(t, err)
		signature, _, err := keyring.Sign(fibre.DefaultKeyName, signBytes, txsigning.SignMode_SIGN_MODE_DIRECT)
		require.NoError(t, err)

		// update proto with new signature
		promisePb.Signature = signature
	}

	promise := &fibre.PaymentPromise{
		ChainID:           "celestia",
		Height:            100,
		Namespace:         testNamespace,
		UploadSize:        uint32(blob.UploadSize()),
		BlobVersion:       uint32(blob.ID().Version()),
		Commitment:        blob.ID().Commitment(),
		CreationTimestamp: time.Now(),
		SignerKey:         pubKey.(*secp256k1.PubKey),
	}

	promisePb, err := promise.ToProto()
	require.NoError(t, err)
	signPromise(promisePb)

	// get row assignment for server validator
	serverCfg := fibre.DefaultServerConfig()
	blobCfg := blob.Config()
	totalRows := blobCfg.OriginalRows + blobCfg.ParityRows
	shardMap := valSet.Assign(blob.ID().Commitment(), totalRows, blobCfg.OriginalRows, serverCfg.MinRowsPerValidator, serverCfg.LivenessThreshold)
	rowIndices := shardMap[serverValidator]
	require.NotEmpty(t, rowIndices, "server validator has no rows assigned")

	// create rows with proofs
	rows := make([]*types.BlobRow, 0, len(rowIndices))
	require.NoError(t, blob.RowProofs(rowIndices, func(index int, row []byte, proof [][]byte) {
		rows = append(rows, &types.BlobRow{
			Index: uint32(index),
			Data:  row,
			Proof: proof,
		})
	}))

	req := &types.UploadShardRequest{
		Promise: promisePb,
		Shard: &types.BlobShard{
			Rows: rows,
			Rlcs: rlc.Marshal(blob.RLC()),
		},
	}

	// apply request modifier after construction
	if requestModifier != nil {
		requestModifier(req)
		// automatically re-sign the promise after modification, unless signature was explicitly removed
		if len(req.Promise.Signature) > 0 {
			signPromise(req.Promise)
		}
	}

	return req
}
