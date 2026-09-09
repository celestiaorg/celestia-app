package fibre

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
	"github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	pebbledb "github.com/cockroachdb/pebble/v2"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestObjectBackendRejectsInvalidReadRPS(t *testing.T) {
	for _, rps := range []float64{0, -1, math.NaN(), math.Inf(1), math.Inf(-1), math.MaxFloat64} {
		t.Run(fmt.Sprint(rps), func(t *testing.T) {
			backend, err := newObjectBackend(&s3ObjectClientStub{}, "bucket", "prefix", "chain", "validator", rps)
			require.Error(t, err)
			require.Nil(t, backend)
		})
	}
}

func TestObjectBackendGetRateLimit(t *testing.T) {
	shard := &types.BlobShard{Rows: []*types.BlobRow{{Data: []byte("data")}}}
	var encoded bytes.Buffer
	require.NoError(t, writeShardBinary(&encoded, shard))
	for _, providerErr := range []error{nil, errors.New("provider error"), &smithy.GenericAPIError{Code: "NoSuchKey"}} {
		t.Run(fmt.Sprint(providerErr), func(t *testing.T) {
			var calls atomic.Int32
			client := &s3ObjectClientStub{
				getObject: func(context.Context, *s3.GetObjectInput, ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
					calls.Add(1)
					if providerErr != nil {
						return nil, providerErr
					}
					return &s3.GetObjectOutput{Body: io.NopCloser(bytes.NewReader(encoded.Bytes()))}, nil
				},
			}
			// One token, with no refill during the test.
			backend, err := newObjectBackend(client, "bucket", "prefix", "chain", "validator", 1e-9)
			require.NoError(t, err)
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			_, err = backend.Get(ctx, Commitment{}, []byte{1})
			require.ErrorIs(t, err, context.Canceled)
			require.Zero(t, calls.Load())

			const readers = 16
			results := make(chan error, readers)
			var wg sync.WaitGroup
			for i := range readers {
				wg.Go(func() {
					_, err := backend.Get(t.Context(), Commitment{byte(i)}, []byte{byte(i)})
					results <- err
				})
			}
			wg.Wait()
			close(results)
			limited := 0
			for err := range results {
				switch {
				case errors.Is(err, ErrObjectReadRateLimited):
					limited++
				case isObjectNotFound(providerErr):
					require.ErrorIs(t, err, ErrStoreNotFound)
				default:
					require.ErrorIs(t, err, providerErr)
				}
			}
			require.Equal(t, readers-1, limited)
			require.Equal(t, int32(1), calls.Load())
		})
	}
}

func TestServerDownloadShardObjectReadLimit(t *testing.T) {
	store := newMarkerTestStore(t)
	var calls int
	client := &s3ObjectClientStub{
		getObject: func(context.Context, *s3.GetObjectInput, ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
			calls++
			return nil, errors.New("provider error")
		},
	}
	object, err := newObjectBackend(client, "bucket", "prefix", "chain", "validator", 1e-9)
	require.NoError(t, err)
	store.shards = newRoutedStorage(object, store.shards.primary)
	commitment := Commitment{1}
	marker := encodeShardMarkerForBackend(objectBackendTag, 1)
	for _, hash := range [][]byte{{1}, {2}} {
		require.NoError(t, store.db.Set(shardKey(commitment, hash), marker, pebbledb.NoSync))
	}
	metrics, err := newServerMetrics(otel.Meter("test"), newOccupancy(0))
	require.NoError(t, err)
	server := &Server{
		store:   store,
		log:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		tracer:  otel.Tracer("test"),
		metrics: metrics,
	}
	req := &types.DownloadShardRequest{BlobId: NewBlobID(0, commitment)}
	response, err := server.DownloadShard(t.Context(), req)
	require.Nil(t, response)
	require.Equal(t, codes.ResourceExhausted, status.Code(err))
	require.Equal(t, 1, calls)

	// An exhausted object backend must not prevent a local sibling from serving the read.
	localHash := []byte{3}
	size := writeMarkerTestShard(t, store, commitment, localHash)
	require.NoError(t, store.db.Set(shardKey(commitment, localHash), encodeShardMarkerForBackend(localBackendTag, size), pebbledb.NoSync))
	response, err = server.DownloadShard(t.Context(), req)
	require.NoError(t, err)
	require.Equal(t, []byte("data"), response.Shard.Rows[0].Data)
	require.Equal(t, 1, calls)
	for _, hash := range [][]byte{{1}, {2}} {
		data, closer, err := store.db.Get(shardKey(commitment, hash))
		require.NoError(t, err)
		require.Equal(t, marker, data)
		require.NoError(t, closer.Close())
		require.NoError(t, store.db.Delete(shardKey(commitment, hash), pebbledb.NoSync))
	}

	response, err = server.DownloadShard(t.Context(), req)
	require.NoError(t, err)
	require.Equal(t, []byte("data"), response.Shard.Rows[0].Data)
	require.Equal(t, 1, calls)
}

func TestObjectBackendPutContentLength(t *testing.T) {
	shard := &types.BlobShard{Rows: []*types.BlobRow{{Data: bytes.Repeat([]byte("data"), 1<<19)}}}
	var encoded bytes.Buffer
	require.NoError(t, writeShardBinary(&encoded, shard))
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil || !bytes.Equal(encoded.Bytes(), body) {
			t.Error("upload body does not match the encoded shard", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if r.ContentLength != int64(encoded.Len()) {
			t.Errorf("content length: got %d, want %d", r.ContentLength, encoded.Len())
		}
	}))
	defer server.Close()
	client := s3.New(s3.Options{
		Region:       "us-east-1",
		Credentials:  credentials.NewStaticCredentialsProvider("test", "test", ""),
		BaseEndpoint: aws.String(server.URL),
		UsePathStyle: true,
		HTTPClient:   server.Client(),
	})
	backend, err := newObjectBackend(client, "bucket", "prefix", "chain", "validator", 1)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	created, err := backend.Put(ctx, Commitment{}, []byte{1}, shard)
	require.NoError(t, err)
	require.True(t, created)
}

func TestObjectBackendPut(t *testing.T) {
	var commitment Commitment
	commitment[0] = 1
	promiseHash := []byte{2, 3}
	shard := &types.BlobShard{
		Rlcs: []byte("rlcs"),
		Rows: []*types.BlobRow{{Index: 4, Data: []byte("data")}},
	}
	wantKey := "fibre/test-chain/celestiavalcons1validator/shards/" + commitment.String() + "-0203"

	t.Run("created", func(t *testing.T) {
		client := &s3ObjectClientStub{
			putObject: func(_ context.Context, input *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
				require.Equal(t, "bucket", aws.ToString(input.Bucket))
				require.Equal(t, wantKey, aws.ToString(input.Key))
				require.Equal(t, "*", aws.ToString(input.IfNoneMatch))
				require.Equal(t, shardBinarySize(shard), aws.ToInt64(input.ContentLength))

				var options s3.Options
				for _, fn := range optFns {
					fn(&options)
				}
				require.Equal(t, aws.RequestChecksumCalculationWhenRequired, options.RequestChecksumCalculation)

				data, err := io.ReadAll(input.Body)
				require.NoError(t, err)
				got, err := readShardBinary(bytes.NewReader(data))
				require.NoError(t, err)
				require.Equal(t, shard, got)
				return &s3.PutObjectOutput{}, nil
			},
		}
		backend, err := newObjectBackend(client, "bucket", "/fibre/", "test-chain", "celestiavalcons1validator", 1)
		require.NoError(t, err)

		created, err := backend.Put(t.Context(), commitment, promiseHash, shard)
		require.NoError(t, err)
		require.True(t, created)
	})

	t.Run("existing", func(t *testing.T) {
		client := &s3ObjectClientStub{
			putObject: func(context.Context, *s3.PutObjectInput, ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
				return nil, &smithy.GenericAPIError{Code: "PreconditionFailed", Fault: smithy.FaultClient}
			},
		}
		backend, err := newObjectBackend(client, "bucket", "fibre", "test-chain", "celestiavalcons1validator", 1)
		require.NoError(t, err)

		created, err := backend.Put(t.Context(), commitment, promiseHash, shard)
		require.NoError(t, err)
		require.False(t, created)
	})
}

func TestObjectBackendGet(t *testing.T) {
	shard := &types.BlobShard{Rows: []*types.BlobRow{{Index: 1, Data: []byte("data")}}}
	var data bytes.Buffer
	require.NoError(t, writeShardBinary(&data, shard))

	client := &s3ObjectClientStub{
		getObject: func(_ context.Context, input *s3.GetObjectInput, _ ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
			require.Equal(t, "bucket", aws.ToString(input.Bucket))
			return &s3.GetObjectOutput{Body: io.NopCloser(bytes.NewReader(data.Bytes()))}, nil
		},
	}
	backend, err := newObjectBackend(client, "bucket", "prefix", "chain", "validator", 1)
	require.NoError(t, err)

	got, err := backend.Get(t.Context(), Commitment{}, []byte{1})
	require.NoError(t, err)
	require.Equal(t, shard, got)
}

// TestObjectBackendGetChecksum guards against accepting corrupt shards when decoding
// finishes before the AWS SDK reports a checksum error at EOF.
func TestObjectBackendGetChecksum(t *testing.T) {
	for _, size := range []int{4, 2 << 20} {
		for _, outcome := range []string{"valid", "corrupt", "trailing data"} {
			t.Run(fmt.Sprintf("%d/%s", size, outcome), func(t *testing.T) {
				shard := &types.BlobShard{Rows: []*types.BlobRow{{Data: bytes.Repeat([]byte("a"), size)}}}
				var encoded bytes.Buffer
				require.NoError(t, writeShardBinary(&encoded, shard))
				if outcome == "trailing data" {
					encoded.WriteByte(0)
				}
				checksum := sha256.Sum256(encoded.Bytes())
				if outcome == "corrupt" {
					encoded.Bytes()[20] ^= 1 // First row data byte, after the length prefixes.
				}
				server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("X-Amz-Checksum-Sha256", base64.StdEncoding.EncodeToString(checksum[:]))
					_, _ = w.Write(encoded.Bytes())
				}))
				defer server.Close()
				client := s3.New(s3.Options{
					Region:                     "us-east-1",
					Credentials:                credentials.NewStaticCredentialsProvider("test", "test", ""),
					BaseEndpoint:               aws.String(server.URL),
					UsePathStyle:               true,
					HTTPClient:                 server.Client(),
					ResponseChecksumValidation: aws.ResponseChecksumValidationWhenSupported,
				})
				backend, err := newObjectBackend(client, "bucket", "prefix", "chain", "validator", 1)
				require.NoError(t, err)
				ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
				defer cancel()
				got, err := backend.Get(ctx, Commitment{}, []byte{1})
				switch outcome {
				case "valid":
					require.NoError(t, err)
					require.Equal(t, shard, got)
				case "corrupt":
					require.ErrorContains(t, err, "checksum did not match")
					require.Nil(t, got)
				case "trailing data":
					require.ErrorContains(t, err, "unexpected trailing data")
					require.Nil(t, got)
				}
			})
		}
	}
}

func TestObjectBackendGetDoesNotRetry(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(w, "<Error><Code>SlowDown</Code><Message>retry later</Message></Error>")
	}))
	defer server.Close()
	client := s3.New(s3.Options{
		Region:           "us-east-1",
		Credentials:      credentials.NewStaticCredentialsProvider("test", "test", ""),
		BaseEndpoint:     aws.String(server.URL),
		UsePathStyle:     true,
		HTTPClient:       server.Client(),
		RetryMaxAttempts: 3,
	})
	backend, err := newObjectBackend(client, "bucket", "prefix", "chain", "validator", 1)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	_, err = backend.Get(ctx, Commitment{}, []byte{1})
	require.ErrorContains(t, err, "SlowDown")
	require.Equal(t, int32(1), calls.Load())
}

func TestObjectBackendNormalisesMissingObject(t *testing.T) {
	missing := &smithy.GenericAPIError{Code: "NoSuchKey", Fault: smithy.FaultClient}
	client := &s3ObjectClientStub{
		getObject: func(context.Context, *s3.GetObjectInput, ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
			return nil, missing
		},
		headObject: func(context.Context, *s3.HeadObjectInput, ...func(*s3.Options)) (*s3.HeadObjectOutput, error) {
			return nil, missing
		},
		deleteObject: func(context.Context, *s3.DeleteObjectInput, ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
			return nil, missing
		},
	}
	backend, err := newObjectBackend(client, "bucket", "prefix", "chain", "validator", 1)
	require.NoError(t, err)

	_, err = backend.Get(t.Context(), Commitment{}, []byte{1})
	require.ErrorIs(t, err, ErrStoreNotFound)
	has, err := backend.Has(t.Context(), Commitment{}, []byte{1})
	require.NoError(t, err)
	require.False(t, has)
	require.NoError(t, backend.Delete(t.Context(), Commitment{}, []byte{1}))
}

func TestObjectBackendDeleteObjects(t *testing.T) {
	var (
		first  Commitment
		second Commitment
	)
	first[0] = 1
	second[0] = 2
	ids := []shardID{
		{commitment: first, promiseHash: []byte{1}},
		{commitment: second, promiseHash: []byte{2}},
	}

	t.Run("individual error", func(t *testing.T) {
		client := &s3ObjectClientStub{
			deleteObjects: func(_ context.Context, input *s3.DeleteObjectsInput, _ ...func(*s3.Options)) (*s3.DeleteObjectsOutput, error) {
				require.Len(t, input.Delete.Objects, 2)
				require.True(t, aws.ToBool(input.Delete.Quiet))
				return &s3.DeleteObjectsOutput{Errors: []s3types.Error{{
					Key:     input.Delete.Objects[1].Key,
					Code:    aws.String("AccessDenied"),
					Message: aws.String("denied"),
				}}}, nil
			},
		}
		backend, err := newObjectBackend(client, "bucket", "prefix", "chain", "validator", 1)
		require.NoError(t, err)

		errs, err := backend.DeleteObjects(t.Context(), ids)
		require.NoError(t, err)
		require.NoError(t, errs[0])
		require.ErrorContains(t, errs[1], "AccessDenied")
	})

	t.Run("request error", func(t *testing.T) {
		requestErr := errors.New("request failed")
		client := &s3ObjectClientStub{
			deleteObjects: func(context.Context, *s3.DeleteObjectsInput, ...func(*s3.Options)) (*s3.DeleteObjectsOutput, error) {
				return nil, requestErr
			},
		}
		backend, err := newObjectBackend(client, "bucket", "prefix", "chain", "validator", 1)
		require.NoError(t, err)

		errs, err := backend.DeleteObjects(t.Context(), ids)
		require.ErrorIs(t, err, requestErr)
		require.Nil(t, errs)
	})

	t.Run("batch limit", func(t *testing.T) {
		backend, err := newObjectBackend(&s3ObjectClientStub{}, "bucket", "prefix", "chain", "validator", 1)
		require.NoError(t, err)
		_, err = backend.DeleteObjects(t.Context(), make([]shardID, maxObjectDeleteBatchSize+1))
		require.ErrorContains(t, err, "maximum is 1000")
	})
}

type s3ObjectClientStub struct {
	putObject     func(context.Context, *s3.PutObjectInput, ...func(*s3.Options)) (*s3.PutObjectOutput, error)
	getObject     func(context.Context, *s3.GetObjectInput, ...func(*s3.Options)) (*s3.GetObjectOutput, error)
	headObject    func(context.Context, *s3.HeadObjectInput, ...func(*s3.Options)) (*s3.HeadObjectOutput, error)
	deleteObject  func(context.Context, *s3.DeleteObjectInput, ...func(*s3.Options)) (*s3.DeleteObjectOutput, error)
	deleteObjects func(context.Context, *s3.DeleteObjectsInput, ...func(*s3.Options)) (*s3.DeleteObjectsOutput, error)
}

func (s *s3ObjectClientStub) PutObject(ctx context.Context, input *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	return s.putObject(ctx, input, optFns...)
}

func (s *s3ObjectClientStub) GetObject(ctx context.Context, input *s3.GetObjectInput, optFns ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	return s.getObject(ctx, input, optFns...)
}

func (s *s3ObjectClientStub) HeadObject(ctx context.Context, input *s3.HeadObjectInput, optFns ...func(*s3.Options)) (*s3.HeadObjectOutput, error) {
	return s.headObject(ctx, input, optFns...)
}

func (s *s3ObjectClientStub) DeleteObject(ctx context.Context, input *s3.DeleteObjectInput, optFns ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
	return s.deleteObject(ctx, input, optFns...)
}

func (s *s3ObjectClientStub) DeleteObjects(ctx context.Context, input *s3.DeleteObjectsInput, optFns ...func(*s3.Options)) (*s3.DeleteObjectsOutput, error) {
	return s.deleteObjects(ctx, input, optFns...)
}
