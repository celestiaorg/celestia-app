package fibre_test

import (
	"crypto/ed25519"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v6/fibre"
	"github.com/celestiaorg/celestia-app/v6/fibre/validator"
	"github.com/celestiaorg/celestia-app/v6/x/fibre/types"
	"github.com/celestiaorg/go-square/v3/share"
	"github.com/celestiaorg/rsema1d"
	"github.com/celestiaorg/rsema1d/field"
	"github.com/cometbft/cometbft/crypto"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	coregrpc "github.com/cometbft/cometbft/rpc/grpc"
	core "github.com/cometbft/cometbft/types"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	txsigning "github.com/cosmos/cosmos-sdk/types/tx/signing"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// TestServerUploadRows unit tests the [Server.UploadRows].
// It currently covers random cases and should be eventually extended for 100% coverage.
// The request modifier approach should allow simulating any failure.
func TestServerUploadRows(t *testing.T) {
	server, valSet, serverValidator := makeTestServer(t)

	tests := []struct {
		name            string
		requestModifier func(*types.UploadRowsRequest)
		check           func(*testing.T, *types.UploadRowsResponse, error)
	}{
		{
			name:            "Success",
			requestModifier: nil,
			check: func(t *testing.T, resp *types.UploadRowsResponse, err error) {
				require.NoError(t, err)
				require.NotNil(t, resp)
				require.NotEmpty(t, resp.ValidatorSignature)
				require.Len(t, resp.ValidatorSignature, ed25519.SignatureSize)
			},
		},
		{
			name: "InvalidPaymentPromise",
			requestModifier: func(req *types.UploadRowsRequest) {
				// invalidate promise by removing signature
				req.Promise.Signature = nil
			},
			check: func(t *testing.T, resp *types.UploadRowsResponse, err error) {
				require.Error(t, err)
				require.Contains(t, err.Error(), "payment promise validation failed")
			},
		},
		{
			name: "WrongChainID",
			requestModifier: func(req *types.UploadRowsRequest) {
				// set wrong chain ID
				req.Promise.ChainId = "wrong-chain"
			},
			check: func(t *testing.T, resp *types.UploadRowsResponse, err error) {
				require.Error(t, err)
				require.Contains(t, err.Error(), "chain ID mismatch")
			},
		},
		{
			name: "TimestampTooOld",
			requestModifier: func(req *types.UploadRowsRequest) {
				// set timestamp 2 hours ago (exceeds default 1 hour PaymentPromiseTimeout)
				req.Promise.CreationTimestamp = time.Now().Add(-2 * time.Hour)
			},
			check: func(t *testing.T, resp *types.UploadRowsResponse, err error) {
				require.Error(t, err)
				require.Contains(t, err.Error(), "payment promise expired")
			},
		},
		{
			name: "InvalidRowAssignment",
			requestModifier: func(req *types.UploadRowsRequest) {
				// replace with another validator's rows
				totalRows := server.Config().OriginalRows + server.Config().ParityRows
				// get commitment from the request (it's already a byte slice)
				var commitment rsema1d.Commitment
				copy(commitment[:], req.Promise.Commitment)
				shardMap := valSet.Assign(commitment, totalRows)
				for val, indices := range shardMap {
					if val.Address.String() != serverValidator.Address.String() && len(indices) > 0 {
						req.Rows.Rows[0].Index = uint32(indices[0])
						break
					}
				}
			},
			check: func(t *testing.T, resp *types.UploadRowsResponse, err error) {
				require.Error(t, err)
				require.Contains(t, err.Error(), "row assignment verification failed")
			},
		},
		{
			name: "InvalidRowProof",
			requestModifier: func(req *types.UploadRowsRequest) {
				// corrupt the proof
				req.Rows.Rows[0].Proof[0] = []byte("invalid proof")
			},
			check: func(t *testing.T, resp *types.UploadRowsResponse, err error) {
				require.Error(t, err)
				require.Contains(t, err.Error(), "verification failed")
			},
		},
		{
			name: "MissingRows",
			requestModifier: func(req *types.UploadRowsRequest) {
				// remove all rows
				req.Rows.Rows = nil
			},
			check: func(t *testing.T, resp *types.UploadRowsResponse, err error) {
				require.Error(t, err)
			},
		},
		{
			name: "InvalidUploadSize",
			requestModifier: func(req *types.UploadRowsRequest) {
				// set wrong upload size
				req.Promise.BlobSize = 12345
			},
			check: func(t *testing.T, resp *types.UploadRowsResponse, err error) {
				require.Error(t, err)
				require.Contains(t, err.Error(), "upload size mismatch")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := makeTestRequest(t, valSet, serverValidator, tt.requestModifier)
			resp, err := server.UploadRows(t.Context(), req)
			tt.check(t, resp, err)
		})
	}
}

// makeTestServer creates a server with all necessary test infrastructure.
func makeTestServer(t *testing.T) (*fibre.Server, validator.Set, *core.Validator) {
	t.Helper()

	// create validator set (use enough validators for good distribution)
	validators, privKeys := makeTestValidators(t, 100)
	valSet := validator.Set{
		ValidatorSet: core.NewValidatorSet(validators),
		Height:       100,
	}

	cfg := fibre.DefaultServerConfig()
	// Set a temporary directory for the BadgerDB store
	tmpDir := t.TempDir()
	cfg.StoreConfig.Path = filepath.Join(tmpDir, "fibre-store")

	// use first validator as the server's identity
	privVal := newTestPrivValidator(privKeys[0])

	// Find the server validator in the ValidatorSet by matching the address
	// Note: core.NewValidatorSet may reorder validators, so we can't assume validators[0] == privKeys[0]
	serverPubKey, err := privVal.GetPubKey()
	require.NoError(t, err)
	serverAddress := serverPubKey.Address()

	serverValidator, found := valSet.GetByAddress(serverAddress)
	require.True(t, found, "server validator not found in validator set")
	require.NotNil(t, serverValidator, "server validator is nil")

	// Create gRPC server with mock services
	grpcServer := grpc.NewServer()

	// Register mock Query service
	mockQueryServer := &mockQueryServer{}
	types.RegisterQueryServer(grpcServer, mockQueryServer)

	// Register mock BlockAPI service
	valSetProto, err := valSet.ValidatorSet.ToProto()
	require.NoError(t, err)
	mockBlockAPIServer := &mockBlockAPIServer{
		validatorSetResponse: &coregrpc.ValidatorSetResponse{
			ValidatorSet: valSetProto,
			Height:       int64(valSet.Height),
		},
	}
	coregrpc.RegisterBlockAPIServer(grpcServer, mockBlockAPIServer)

	// Create in-memory listener
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	// Create client connection to the mock server (will connect after server starts)
	conn, err := grpc.NewClient(
		listener.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)

	// Create server with gRPC infrastructure - this registers the Fibre service
	server, err := fibre.NewServerFromGRPC(privVal, grpcServer, conn, cfg)
	require.NoError(t, err)

	// Start gRPC server after all services are registered
	go func() {
		if err := grpcServer.Serve(listener); err != nil {
			t.Logf("gRPC server error: %v", err)
		}
	}()

	return server, valSet, serverValidator
}

// makeTestRequest creates a valid UploadRowsRequest for the given test setup.
// Optional modifier can be provided to customize the request after construction.
// The promise is automatically re-signed after modification.
func makeTestRequest(
	t *testing.T,
	valSet validator.Set,
	serverValidator *core.Validator,
	requestModifier func(*types.UploadRowsRequest),
) *types.UploadRowsRequest {
	t.Helper()

	blob := makeTestBlobV0(t, 256*1024)
	blobCfg := fibre.DefaultBlobConfigV0()
	namespace := share.MustNewV0Namespace([]byte("testns"))

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
		Namespace:         namespace,
		UploadSize:        uint32(blob.UploadSize()),
		BlobVersion:       0,
		Commitment:        blob.Commitment(),
		CreationTimestamp: time.Now(),
		SignerKey:         pubKey.(*secp256k1.PubKey),
	}

	promisePb := promise.ToProto()
	signPromise(promisePb)

	// get row assignment for server validator
	totalRows := blobCfg.OriginalRows + blobCfg.ParityRows
	shardMap := valSet.Assign(rsema1d.Commitment(blob.Commitment()), totalRows)
	rowIndices := shardMap[serverValidator]
	require.NotEmpty(t, rowIndices, "server validator has no rows assigned")

	// create rows with proofs
	rows := make([]*types.Row, len(rowIndices))
	for i, rowIndex := range rowIndices {
		rowProof, err := blob.Row(rowIndex)
		require.NoError(t, err)
		rows[i] = &types.Row{
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

	req := &types.UploadRowsRequest{
		Promise: promisePb,
		Rows: &types.Rows{
			Rows: rows,
			Rlc:  &types.Rows_Coefficients{Coefficients: rlcCoeffsBytes},
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

// testPrivValidator is a simple mock PrivValidator for testing.
type testPrivValidator struct {
	privKey crypto.PrivKey
}

func newTestPrivValidator(privKey crypto.PrivKey) *testPrivValidator {
	return &testPrivValidator{privKey: privKey}
}

func (m *testPrivValidator) GetPubKey() (crypto.PubKey, error) {
	return m.privKey.PubKey(), nil
}

func (m *testPrivValidator) SignRawBytes(chainID, uniqueID string, rawBytes []byte) ([]byte, error) {
	return m.privKey.Sign(rawBytes)
}

func (m *testPrivValidator) SignVote(chainID string, vote *cmtproto.Vote) error {
	return nil
}

func (m *testPrivValidator) SignProposal(chainID string, proposal *cmtproto.Proposal) error {
	return nil
}

func (m *testPrivValidator) GetAddress() core.Address {
	return m.privKey.PubKey().Address()
}
