package fibre

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
	"github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	"github.com/stretchr/testify/require"
)

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
	backend := newObjectBackend(client, "bucket", "prefix", "chain", "validator")
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
		backend := newObjectBackend(client, "bucket", "/fibre/", "test-chain", "celestiavalcons1validator")

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
		backend := newObjectBackend(client, "bucket", "fibre", "test-chain", "celestiavalcons1validator")

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
	backend := newObjectBackend(client, "bucket", "prefix", "chain", "validator")

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
				backend := newObjectBackend(client, "bucket", "prefix", "chain", "validator")
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
	backend := newObjectBackend(client, "bucket", "prefix", "chain", "validator")

	_, err := backend.Get(t.Context(), Commitment{}, []byte{1})
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
		backend := newObjectBackend(client, "bucket", "prefix", "chain", "validator")

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
		backend := newObjectBackend(client, "bucket", "prefix", "chain", "validator")

		errs, err := backend.DeleteObjects(t.Context(), ids)
		require.ErrorIs(t, err, requestErr)
		require.Nil(t, errs)
	})

	t.Run("batch limit", func(t *testing.T) {
		backend := newObjectBackend(&s3ObjectClientStub{}, "bucket", "prefix", "chain", "validator")
		_, err := backend.DeleteObjects(t.Context(), make([]shardID, maxObjectDeleteBatchSize+1))
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
