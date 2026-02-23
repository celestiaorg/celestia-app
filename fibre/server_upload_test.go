package fibre_test

import (
	"crypto/ed25519"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app-fibre/v6/fibre"
	"github.com/celestiaorg/celestia-app-fibre/v6/fibre/validator"
	"github.com/celestiaorg/celestia-app-fibre/v6/x/fibre/types"
	"github.com/celestiaorg/rsema1d"
	"github.com/celestiaorg/rsema1d/field"
	core "github.com/cometbft/cometbft/types"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	txsigning "github.com/cosmos/cosmos-sdk/types/tx/signing"
	"github.com/stretchr/testify/require"
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := makeTestRequest(t, valSet, serverValidator, tt.requestModifier)
			resp, err := server.UploadShard(t.Context(), req)
			tt.check(t, resp, err)
		})
	}
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

	promisePb := promise.ToProto()
	signPromise(promisePb)

	// get row assignment for server validator
	serverCfg := fibre.DefaultServerConfig()
	blobCfg := blob.Config()
	totalRows := blobCfg.OriginalRows + blobCfg.ParityRows
	shardMap := valSet.Assign(blob.ID().Commitment(), totalRows, blobCfg.OriginalRows, serverCfg.MinRowsPerValidator, serverCfg.LivenessThreshold)
	rowIndices := shardMap[serverValidator]
	require.NotEmpty(t, rowIndices, "server validator has no rows assigned")

	// create rows with proofs
	rows := make([]*types.BlobRow, len(rowIndices))
	for i, rowIndex := range rowIndices {
		rowProof, err := blob.Row(rowIndex)
		require.NoError(t, err)
		rows[i] = &types.BlobRow{
			Index: uint32(rowIndex),
			Data:  rowProof.Row,
			Proof: rowProof.RowProof.RowProof,
		}
	}

	// flatten RLC coefficients
	rlcCoeffs := blob.RLCCoeffs()
	rlcCoeffsBytes := make([]byte, len(rlcCoeffs)*16)
	for i, coeff := range rlcCoeffs {
		b := field.ToBytes128(coeff)
		copy(rlcCoeffsBytes[i*16:(i+1)*16], b[:])
	}

	req := &types.UploadShardRequest{
		Promise: promisePb,
		Shard: &types.BlobShard{
			Rows: rows,
			Rlc:  &types.BlobShard_Coefficients{Coefficients: rlcCoeffsBytes},
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
