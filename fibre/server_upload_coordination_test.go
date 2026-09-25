package fibre

import (
	"context"
	"encoding/hex"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v10/fibre/state"
	"github.com/celestiaorg/celestia-app/v10/fibre/validator"
	"github.com/celestiaorg/celestia-app/v10/pkg/rsema1d/rlc"
	"github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	"github.com/celestiaorg/go-square/v4/share"
	core "github.com/cometbft/cometbft/types"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/metric/noop"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestUploadIndependentPromises(t *testing.T) {
	server, request := newCoordinatedUploadTest(t)
	first := request(0)
	var promise PaymentPromise
	require.NoError(t, promise.FromProto(first.Promise))
	firstHash, err := promise.Hash()
	require.NoError(t, err)
	var second *types.UploadShardRequest
	for i := 1; i < 10000; i++ {
		candidate := request(i)
		var p PaymentPromise
		require.NoError(t, p.FromProto(candidate.Promise))
		hash, err := p.Hash()
		require.NoError(t, err)
		if hash[0] == firstHash[0] {
			require.NotEqual(t, firstHash, hash)
			second = candidate
			break
		}
	}
	require.NotNil(t, second, "must find a first-byte collision")
	backend := &blockedUploadBackend{shardBackend: server.store.shards.primary, hash: hex.EncodeToString(firstHash), entered: make(chan struct{}), release: make(chan struct{})}
	server.store.shards.primary = backend
	firstDone := startTestUpload(server, t.Context(), first)
	var secondDone <-chan error
	defer func() {
		close(backend.release)
		require.NoError(t, <-firstDone)
		if secondDone != nil {
			require.NoError(t, <-secondDone)
		}
	}()
	select {
	case <-backend.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("first upload did not enter storage")
	}
	secondDone = startTestUpload(server, t.Context(), second)
	select {
	case err := <-secondDone:
		secondDone = nil
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("different promise with the same first hash byte blocked behind storage")
	}
	require.Equal(t, 2*shardBinarySize(first.Shard), server.occ.usage())
}

func TestUploadDuplicateAndFailedOwner(t *testing.T) {
	for _, fail := range []bool{false, true} {
		name := "duplicate"
		if fail {
			name = "failed owner"
		}
		t.Run(name, func(t *testing.T) {
			server, request := newCoordinatedUploadTest(t)
			req := request(0)
			var p PaymentPromise
			require.NoError(t, p.FromProto(req.Promise))
			hash, err := p.Hash()
			require.NoError(t, err)
			backend := &blockedUploadBackend{shardBackend: server.store.shards.primary, hash: hex.EncodeToString(hash), entered: make(chan struct{}), release: make(chan struct{}), fail: fail}
			server.store.shards.primary = backend
			server.occ.setBudget(shardBinarySize(req.Shard))
			first := startTestUpload(server, t.Context(), req)
			<-backend.entered
			second := startTestUpload(server, t.Context(), req)
			close(backend.release)
			firstErr := <-first
			if fail {
				require.Equal(t, codes.Internal, status.Code(firstErr))
			} else {
				require.NoError(t, firstErr)
			}
			require.NoError(t, <-second)
			wantPuts := int32(1)
			if fail {
				wantPuts++
			}
			require.Equal(t, wantPuts, backend.puts.Load())
			require.Equal(t, shardBinarySize(req.Shard), server.occ.usage())
			require.Empty(t, server.uploads.owners)
			size, err := server.store.Size(t.Context())
			require.NoError(t, err)
			require.Equal(t, size, server.occ.usage())
		})
	}
}

type blockedUploadBackend struct {
	shardBackend
	hash    string
	entered chan struct{}
	release chan struct{}
	once    sync.Once
	puts    atomic.Int32
	fail    bool
}

func (b *blockedUploadBackend) Put(ctx context.Context, commitment Commitment, hash []byte, shard *types.BlobShard) error {
	b.puts.Add(1)
	blocked := false
	if hex.EncodeToString(hash) == b.hash {
		b.once.Do(func() { blocked = true; close(b.entered); <-b.release })
	}
	if blocked && b.fail {
		return errors.New("storage failed")
	}

	return b.shardBackend.Put(ctx, commitment, hash, shard)
}

func startTestUpload(s *Server, ctx context.Context, req *types.UploadShardRequest) <-chan error {
	done := make(chan error, 1)
	go func() { _, err := s.UploadShard(ctx, req); done <- err }()
	return done
}

type uploadTestState struct {
	state.Client
	set validator.Set
}

func (*uploadTestState) ChainID() string { return "celestia" }
func (s *uploadTestState) GetByHeight(context.Context, uint64) (validator.Set, error) {
	return s.set, nil
}

func (*uploadTestState) VerifyPromise(_ context.Context, p *state.PaymentPromise) (state.VerifiedPromise, error) {
	return state.VerifiedPromise{ExpiresAt: p.CreationTimestamp.Add(time.Hour)}, nil
}

func newCoordinatedUploadTest(t *testing.T) (*Server, func(int) *types.UploadShardRequest) {
	t.Helper()
	signer := core.NewMockPV()
	pub, err := signer.GetPubKey()
	require.NoError(t, err)
	val := core.NewValidator(pub, 1)
	set := validator.Set{ValidatorSet: core.NewValidatorSet([]*core.Validator{val}), Height: 100}
	cfg := DefaultServerConfig()
	occ := newOccupancy(0)
	metrics, err := newServerMetrics(noop.NewMeterProvider().Meter("upload-test"), occ)
	require.NoError(t, err)
	server := &Server{Config: cfg, state: &uploadTestState{set: set}, signer: signer, store: newMarkerTestStore(t), occ: occ, metrics: metrics, log: slog.Default(), tracer: tracenoop.NewTracerProvider().Tracer("upload-test"), verifiers: newVerifierPool(2)}
	blob, err := NewBlob([]byte("upload coordination"), DefaultBlobConfigV0())
	require.NoError(t, err)
	t.Cleanup(blob.Free)
	indices := set.Assign(blob.ID().Commitment(), blob.Config().TotalRows(), blob.Config().OriginalRows, cfg.MinRowsPerValidator, cfg.LivenessThreshold)[set.Validators[0]]
	shard := &types.BlobShard{Rlcs: rlc.Marshal(blob.RLC())}
	require.NoError(t, blob.RowProofs(indices, func(index int, row []byte, proof [][]byte) {
		shard.Rows = append(shard.Rows, &types.BlobRow{Index: uint32(index), Data: row, Proof: proof})
	}))
	key := secp256k1.GenPrivKeyFromSecret([]byte("upload coordination"))
	return server, func(n int) *types.UploadShardRequest {
		p := &PaymentPromise{ChainID: "celestia", Height: 100, Namespace: share.MustNewV0Namespace([]byte("upload")), UploadSize: uint32(blob.UploadSize()), Commitment: blob.ID().Commitment(), CreationTimestamp: time.Unix(1700000000, int64(n)), SignerKey: key.PubKey().(*secp256k1.PubKey)}
		signBytes, err := p.SignBytes()
		require.NoError(t, err)
		p.Signature, err = key.Sign(signBytes)
		require.NoError(t, err)
		pb, err := p.ToProto()
		require.NoError(t, err)
		return &types.UploadShardRequest{Promise: pb, Shard: shard}
	}
}
