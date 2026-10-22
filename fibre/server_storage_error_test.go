package fibre

import (
	"context"
	"errors"
	"log/slog"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v11/x/fibre/types"
	pebbledb "github.com/cockroachdb/pebble/v2"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestStorageErrorsStayInServerLogs(t *testing.T) {
	for _, stage := range []string{"download", "presence", "locked presence", "write", "cancelled write", "expired write"} {
		t.Run(stage, func(t *testing.T) {
			server, request := newCoordinatedUploadTest(t)
			req := request(0)
			var promise PaymentPromise
			require.NoError(t, promise.FromProto(req.Promise))
			hash, err := promise.Hash()
			require.NoError(t, err)
			const endpoint = "https://private-account.r2.cloudflarestorage.com/private-bucket/private-prefix/shard"
			backendErr := &url.Error{Op: "storage", URL: endpoint, Err: errors.New("connection refused")}
			ctx, cancel := context.WithCancelCause(t.Context())
			defer cancel(nil)
			backend := &failingRPCBackend{shardBackend: server.store.shards.primary, err: backendErr}
			if stage == "cancelled write" {
				backend.onPut = func() { cancel(context.Canceled) }
			}
			if stage == "expired write" {
				backend.onPut = func() { cancel(context.DeadlineExceeded) }
			}
			server.store.shards.primary = backend
			var logs strings.Builder
			server.log = slog.New(slog.NewTextHandler(&logs, nil))
			if !strings.HasSuffix(stage, "write") {
				require.NoError(t, server.store.db.Set(shardKey(promise.Commitment, hash), encodeShardMarkerForBackend(localBackendTag, 1), pebbledb.NoSync))
			}
			var want string
			wantCode := codes.Internal
			switch stage {
			case "download":
				_, err = server.DownloadShard(t.Context(), &types.DownloadShardRequest{BlobId: NewBlobID(0, promise.Commitment)})
				want = "failed to retrieve blob shard"
			case "presence":
				_, err = server.UploadShard(t.Context(), req)
				want = "failed to check if store has the commitment"
			case "locked presence":
				err = server.storeShard(t.Context(), server.log, &promise, hash, time.Now(), req.Shard)
				want = "failed to check if store has the commitment"
			case "write", "cancelled write", "expired write":
				_, err = server.UploadShard(ctx, req)
				want = "failed to store upload data"
				if stage == "cancelled write" {
					want, wantCode = "store upload aborted", codes.Canceled
				}
				if stage == "expired write" {
					want, wantCode = "store upload aborted", codes.DeadlineExceeded
				}
			}
			require.Equal(t, wantCode, status.Code(err))
			require.Equal(t, want, status.Convert(err).Message())
			require.Contains(t, logs.String(), endpoint)
			require.Zero(t, server.occ.usage())
		})
	}
}

type failingRPCBackend struct {
	shardBackend
	err   error
	onPut func()
}

func (b *failingRPCBackend) Get(context.Context, Commitment, []byte) (*types.BlobShard, error) {
	return nil, b.err
}

func (b *failingRPCBackend) Has(context.Context, Commitment, []byte) (bool, error) {
	return false, b.err
}

func (b *failingRPCBackend) Put(context.Context, Commitment, []byte, *types.BlobShard) error {
	if b.onPut != nil {
		b.onPut()
	}
	return b.err
}
