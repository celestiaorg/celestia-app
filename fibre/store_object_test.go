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
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/retry"
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
	backend := newObjectBackend(client, objectNamespace{Bucket: "bucket", Prefix: "prefix", ChainID: "chain", ValidatorAddress: "validator"})
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	err := backend.Put(ctx, Commitment{}, []byte{1}, shard)
	require.NoError(t, err)
}

func TestObjectBackendMultipartHTTPRetryAndConditionalComplete(t *testing.T) {
	shard := &types.BlobShard{Rows: []*types.BlobRow{{Data: bytes.Repeat([]byte("data"), 40)}}}
	var encoded bytes.Buffer
	require.NoError(t, writeShardBinary(&encoded, shard))
	var (
		mu       sync.Mutex
		attempts = make(map[int][]byte)
		parts    = make(map[int][]byte)
	)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		w.Header().Set("Content-Type", "application/xml")
		switch {
		case r.Method == http.MethodPost && query.Has("uploads"):
			_, _ = io.WriteString(w, `<InitiateMultipartUploadResult><UploadId>upload-id</UploadId></InitiateMultipartUploadResult>`)
		case r.Method == http.MethodPut && query.Has("partNumber"):
			partNumber, err := strconv.Atoi(query.Get("partNumber"))
			require.NoError(t, err)
			body, err := io.ReadAll(r.Body)
			require.NoError(t, err)
			mu.Lock()
			previous, retried := attempts[partNumber]
			attempts[partNumber] = append([]byte(nil), body...)
			mu.Unlock()
			if partNumber == 1 && !retried {
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = io.WriteString(w, `<Error><Code>SlowDown</Code></Error>`)
				return
			}
			if retried {
				require.Equal(t, previous, body)
			}
			mu.Lock()
			parts[partNumber] = body
			mu.Unlock()
			w.Header().Set("ETag", fmt.Sprintf(`"etag-%d"`, partNumber))
		case r.Method == http.MethodPost && query.Has("uploadId"):
			require.Equal(t, "*", r.Header.Get("If-None-Match"))
			body, err := io.ReadAll(r.Body)
			require.NoError(t, err)
			require.Contains(t, string(body), "<PartNumber>1</PartNumber>")
			_, _ = io.WriteString(w, `<CompleteMultipartUploadResult><ETag>etag</ETag></CompleteMultipartUploadResult>`)
		case r.Method == http.MethodDelete:
			t.Error("successful multipart upload must not be aborted")
		default:
			t.Errorf("unexpected S3 request: %s %s", r.Method, r.URL.String())
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	defer server.Close()
	client := s3.New(s3.Options{
		Region:       "us-east-1",
		Credentials:  credentials.NewStaticCredentialsProvider("test", "test", ""),
		BaseEndpoint: aws.String(server.URL),
		UsePathStyle: true,
		HTTPClient:   server.Client(),
		Retryer: retry.NewStandard(func(o *retry.StandardOptions) {
			o.MaxAttempts = 2
			o.Backoff = retry.BackoffDelayerFunc(func(int, error) (time.Duration, error) { return 0, nil })
		}),
	})
	backend := newObjectBackend(client, objectNamespace{Bucket: "bucket"})
	backend.multipartThreshold = int64(encoded.Len())
	backend.multipartPartSize = 32
	backend.multipartPartRequests = make(chan struct{}, 2)

	require.NoError(t, backend.Put(t.Context(), Commitment{}, []byte{1}, shard))
	var uploaded bytes.Buffer
	for partNumber := 1; partNumber <= len(parts); partNumber++ {
		uploaded.Write(parts[partNumber])
	}
	require.Equal(t, encoded.Bytes(), uploaded.Bytes())
}

func TestObjectBackendPutRetry(t *testing.T) {
	for _, secondStatus := range []int{http.StatusOK, http.StatusPreconditionFailed, http.StatusServiceUnavailable} {
		t.Run(http.StatusText(secondStatus), func(t *testing.T) {
			shard := &types.BlobShard{
				Rlcs: []byte("rlcs"),
				Rows: []*types.BlobRow{{Index: 4, Data: bytes.Repeat([]byte("data"), 1<<19), Proof: [][]byte{[]byte("proof")}}},
			}
			var encoded bytes.Buffer
			require.NoError(t, writeShardBinary(&encoded, shard))
			var attempts atomic.Int32
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				if err != nil || !bytes.Equal(encoded.Bytes(), body) {
					t.Error("upload body does not match the encoded shard", err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				if r.ContentLength != int64(encoded.Len()) || r.Header.Get("If-None-Match") != "*" {
					t.Error("upload length or conditional creation header changed")
				}
				status := secondStatus
				if attempts.Add(1) == 1 {
					status = http.StatusServiceUnavailable
				}
				w.Header().Set("Content-Type", "application/xml")
				w.WriteHeader(status)
				switch status {
				case http.StatusServiceUnavailable:
					_, _ = io.WriteString(w, "<Error><Code>SlowDown</Code></Error>")
				case http.StatusPreconditionFailed:
					_, _ = io.WriteString(w, "<Error><Code>PreconditionFailed</Code></Error>")
				}
			}))
			defer server.Close()
			client := s3.New(s3.Options{
				Region:       "us-east-1",
				Credentials:  credentials.NewStaticCredentialsProvider("test", "test", ""),
				BaseEndpoint: aws.String(server.URL),
				UsePathStyle: true,
				HTTPClient:   server.Client(),
				Retryer: retry.NewStandard(func(o *retry.StandardOptions) {
					o.MaxAttempts = 2
					o.Backoff = retry.BackoffDelayerFunc(func(int, error) (time.Duration, error) { return 0, nil })
				}),
			})
			backend := newObjectBackend(client, objectNamespace{Bucket: "bucket"})
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			err := backend.Put(ctx, Commitment{}, []byte{1}, shard)
			if secondStatus == http.StatusServiceUnavailable {
				require.True(t, hasObjectErrorCode(err, "SlowDown"), "got %v", err)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, int32(2), attempts.Load())
		})
	}
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
		backend := newObjectBackend(client, objectNamespace{Bucket: "bucket", Prefix: "/fibre/", ChainID: "test-chain", ValidatorAddress: "celestiavalcons1validator"})

		err := backend.Put(t.Context(), commitment, promiseHash, shard)
		require.NoError(t, err)
	})

	t.Run("existing", func(t *testing.T) {
		client := &s3ObjectClientStub{
			putObject: func(context.Context, *s3.PutObjectInput, ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
				return nil, &smithy.GenericAPIError{Code: "PreconditionFailed", Fault: smithy.FaultClient}
			},
		}
		backend := newObjectBackend(client, objectNamespace{Bucket: "bucket", Prefix: "fibre", ChainID: "test-chain", ValidatorAddress: "celestiavalcons1validator"})

		err := backend.Put(t.Context(), commitment, promiseHash, shard)
		require.NoError(t, err)
	})
}

func TestObjectBackendMultipartPut(t *testing.T) {
	shard := &types.BlobShard{
		Rlcs: []byte("rlcs"),
		Rows: []*types.BlobRow{{Index: 4, Data: bytes.Repeat([]byte("data"), 30), Proof: [][]byte{[]byte("proof")}}},
	}
	var encoded bytes.Buffer
	require.NoError(t, writeShardBinary(&encoded, shard))

	var (
		mu             sync.Mutex
		parts          = make(map[int32][]byte)
		completedParts []int32
	)
	client := &s3ObjectClientStub{
		createMultipartUpload: func(_ context.Context, input *s3.CreateMultipartUploadInput, _ ...func(*s3.Options)) (*s3.CreateMultipartUploadOutput, error) {
			require.Equal(t, "bucket", aws.ToString(input.Bucket))
			return &s3.CreateMultipartUploadOutput{UploadId: aws.String("upload-id")}, nil
		},
		uploadPart: func(_ context.Context, input *s3.UploadPartInput, _ ...func(*s3.Options)) (*s3.UploadPartOutput, error) {
			data, err := io.ReadAll(input.Body)
			require.NoError(t, err)
			require.Equal(t, int64(len(data)), aws.ToInt64(input.ContentLength))
			partNumber := aws.ToInt32(input.PartNumber)
			mu.Lock()
			parts[partNumber] = data
			mu.Unlock()
			return &s3.UploadPartOutput{ETag: aws.String(fmt.Sprintf("etag-%d", partNumber))}, nil
		},
		completeMultipartUpload: func(_ context.Context, input *s3.CompleteMultipartUploadInput, _ ...func(*s3.Options)) (*s3.CompleteMultipartUploadOutput, error) {
			require.Equal(t, "*", aws.ToString(input.IfNoneMatch))
			for _, part := range input.MultipartUpload.Parts {
				completedParts = append(completedParts, aws.ToInt32(part.PartNumber))
			}
			return &s3.CompleteMultipartUploadOutput{}, nil
		},
		abortMultipartUpload: func(context.Context, *s3.AbortMultipartUploadInput, ...func(*s3.Options)) (*s3.AbortMultipartUploadOutput, error) {
			t.Fatal("successful multipart upload must not be aborted")
			return nil, nil
		},
	}
	backend := newObjectBackend(client, objectNamespace{Bucket: "bucket", Prefix: "prefix", ChainID: "chain", ValidatorAddress: "validator"})
	backend.multipartThreshold = 32
	backend.multipartPartSize = 32
	backend.multipartPartRequests = make(chan struct{}, 2)

	require.NoError(t, backend.Put(t.Context(), Commitment{}, []byte{1}, shard))
	require.True(t, sort.SliceIsSorted(completedParts, func(i, j int) bool { return completedParts[i] < completedParts[j] }))
	var uploaded bytes.Buffer
	for partNumber := int32(1); partNumber <= int32(len(parts)); partNumber++ {
		uploaded.Write(parts[partNumber])
	}
	require.Equal(t, encoded.Bytes(), uploaded.Bytes())
}

func TestObjectBackendMultipartPutExistingAborts(t *testing.T) {
	var aborted atomic.Bool
	client := &s3ObjectClientStub{
		createMultipartUpload: func(context.Context, *s3.CreateMultipartUploadInput, ...func(*s3.Options)) (*s3.CreateMultipartUploadOutput, error) {
			return &s3.CreateMultipartUploadOutput{UploadId: aws.String("upload-id")}, nil
		},
		uploadPart: func(context.Context, *s3.UploadPartInput, ...func(*s3.Options)) (*s3.UploadPartOutput, error) {
			return &s3.UploadPartOutput{ETag: aws.String("etag")}, nil
		},
		completeMultipartUpload: func(context.Context, *s3.CompleteMultipartUploadInput, ...func(*s3.Options)) (*s3.CompleteMultipartUploadOutput, error) {
			return nil, &smithy.GenericAPIError{Code: "PreconditionFailed", Fault: smithy.FaultClient}
		},
		abortMultipartUpload: func(_ context.Context, input *s3.AbortMultipartUploadInput, _ ...func(*s3.Options)) (*s3.AbortMultipartUploadOutput, error) {
			require.Equal(t, "upload-id", aws.ToString(input.UploadId))
			aborted.Store(true)
			return &s3.AbortMultipartUploadOutput{}, nil
		},
	}
	backend := newObjectBackend(client, objectNamespace{Bucket: "bucket"})
	backend.multipartThreshold = 1
	backend.multipartPartSize = 16
	backend.multipartPartRequests = make(chan struct{}, 1)

	err := backend.Put(t.Context(), Commitment{}, []byte{1}, &types.BlobShard{})
	require.NoError(t, err)
	require.True(t, aborted.Load())
}

func TestObjectBackendMultipartPutFailureAborts(t *testing.T) {
	uploadErr := errors.New("upload failed")
	var aborted atomic.Bool
	client := &s3ObjectClientStub{
		createMultipartUpload: func(context.Context, *s3.CreateMultipartUploadInput, ...func(*s3.Options)) (*s3.CreateMultipartUploadOutput, error) {
			return &s3.CreateMultipartUploadOutput{UploadId: aws.String("upload-id")}, nil
		},
		uploadPart: func(context.Context, *s3.UploadPartInput, ...func(*s3.Options)) (*s3.UploadPartOutput, error) {
			return nil, uploadErr
		},
		abortMultipartUpload: func(context.Context, *s3.AbortMultipartUploadInput, ...func(*s3.Options)) (*s3.AbortMultipartUploadOutput, error) {
			aborted.Store(true)
			return &s3.AbortMultipartUploadOutput{}, nil
		},
	}
	backend := newObjectBackend(client, objectNamespace{Bucket: "bucket"})
	backend.multipartThreshold = 1
	backend.multipartPartSize = 16
	backend.multipartPartRequests = make(chan struct{}, 1)

	err := backend.Put(t.Context(), Commitment{}, []byte{1}, &types.BlobShard{})
	require.ErrorIs(t, err, uploadErr)
	require.True(t, aborted.Load())
}

func TestObjectBackendMultipartPutCancellationUsesCleanupContext(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	var aborted atomic.Bool
	client := &s3ObjectClientStub{
		createMultipartUpload: func(context.Context, *s3.CreateMultipartUploadInput, ...func(*s3.Options)) (*s3.CreateMultipartUploadOutput, error) {
			cancel()
			return &s3.CreateMultipartUploadOutput{UploadId: aws.String("upload-id")}, nil
		},
		uploadPart: func(ctx context.Context, _ *s3.UploadPartInput, _ ...func(*s3.Options)) (*s3.UploadPartOutput, error) {
			return nil, ctx.Err()
		},
		abortMultipartUpload: func(ctx context.Context, _ *s3.AbortMultipartUploadInput, _ ...func(*s3.Options)) (*s3.AbortMultipartUploadOutput, error) {
			require.NoError(t, ctx.Err())
			aborted.Store(true)
			return &s3.AbortMultipartUploadOutput{}, nil
		},
	}
	backend := newObjectBackend(client, objectNamespace{Bucket: "bucket"})
	backend.multipartThreshold = 1
	backend.multipartPartSize = 16
	backend.multipartPartRequests = make(chan struct{}, 1)

	err := backend.Put(ctx, Commitment{}, []byte{1}, &types.BlobShard{})
	require.ErrorIs(t, err, context.Canceled)
	require.True(t, aborted.Load())
}

func TestObjectBackendMultipartPutReportsAbortFailure(t *testing.T) {
	uploadErr := errors.New("upload failed")
	abortErr := errors.New("abort failed")
	client := &s3ObjectClientStub{
		createMultipartUpload: func(context.Context, *s3.CreateMultipartUploadInput, ...func(*s3.Options)) (*s3.CreateMultipartUploadOutput, error) {
			return &s3.CreateMultipartUploadOutput{UploadId: aws.String("upload-id")}, nil
		},
		uploadPart: func(context.Context, *s3.UploadPartInput, ...func(*s3.Options)) (*s3.UploadPartOutput, error) {
			return nil, uploadErr
		},
		abortMultipartUpload: func(context.Context, *s3.AbortMultipartUploadInput, ...func(*s3.Options)) (*s3.AbortMultipartUploadOutput, error) {
			return nil, abortErr
		},
	}
	backend := newObjectBackend(client, objectNamespace{Bucket: "bucket"})
	backend.multipartThreshold = 1
	backend.multipartPartSize = 16
	backend.multipartPartRequests = make(chan struct{}, 1)

	err := backend.Put(t.Context(), Commitment{}, []byte{1}, &types.BlobShard{})
	require.ErrorIs(t, err, uploadErr)
	require.ErrorIs(t, err, abortErr)
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
	backend := newObjectBackend(client, objectNamespace{Bucket: "bucket", Prefix: "prefix", ChainID: "chain", ValidatorAddress: "validator"})

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
				backend := newObjectBackend(client, objectNamespace{Bucket: "bucket", Prefix: "prefix", ChainID: "chain", ValidatorAddress: "validator"})
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
	backend := newObjectBackend(client, objectNamespace{Bucket: "bucket", Prefix: "prefix", ChainID: "chain", ValidatorAddress: "validator"})

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
		backend := newObjectBackend(client, objectNamespace{Bucket: "bucket", Prefix: "prefix", ChainID: "chain", ValidatorAddress: "validator"})

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
		backend := newObjectBackend(client, objectNamespace{Bucket: "bucket", Prefix: "prefix", ChainID: "chain", ValidatorAddress: "validator"})

		errs, err := backend.DeleteObjects(t.Context(), ids)
		require.ErrorIs(t, err, requestErr)
		require.Nil(t, errs)
	})

	t.Run("batch limit", func(t *testing.T) {
		backend := newObjectBackend(&s3ObjectClientStub{}, objectNamespace{Bucket: "bucket", Prefix: "prefix", ChainID: "chain", ValidatorAddress: "validator"})
		_, err := backend.DeleteObjects(t.Context(), make([]shardID, maxObjectDeleteBatchSize+1))
		require.ErrorContains(t, err, "maximum is 1000")
	})
}

type s3ObjectClientStub struct {
	putObject               func(context.Context, *s3.PutObjectInput, ...func(*s3.Options)) (*s3.PutObjectOutput, error)
	createMultipartUpload   func(context.Context, *s3.CreateMultipartUploadInput, ...func(*s3.Options)) (*s3.CreateMultipartUploadOutput, error)
	uploadPart              func(context.Context, *s3.UploadPartInput, ...func(*s3.Options)) (*s3.UploadPartOutput, error)
	completeMultipartUpload func(context.Context, *s3.CompleteMultipartUploadInput, ...func(*s3.Options)) (*s3.CompleteMultipartUploadOutput, error)
	abortMultipartUpload    func(context.Context, *s3.AbortMultipartUploadInput, ...func(*s3.Options)) (*s3.AbortMultipartUploadOutput, error)
	getObject               func(context.Context, *s3.GetObjectInput, ...func(*s3.Options)) (*s3.GetObjectOutput, error)
	headObject              func(context.Context, *s3.HeadObjectInput, ...func(*s3.Options)) (*s3.HeadObjectOutput, error)
	deleteObject            func(context.Context, *s3.DeleteObjectInput, ...func(*s3.Options)) (*s3.DeleteObjectOutput, error)
	deleteObjects           func(context.Context, *s3.DeleteObjectsInput, ...func(*s3.Options)) (*s3.DeleteObjectsOutput, error)
}

func (s *s3ObjectClientStub) PutObject(ctx context.Context, input *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	return s.putObject(ctx, input, optFns...)
}

func (s *s3ObjectClientStub) CreateMultipartUpload(ctx context.Context, input *s3.CreateMultipartUploadInput, optFns ...func(*s3.Options)) (*s3.CreateMultipartUploadOutput, error) {
	return s.createMultipartUpload(ctx, input, optFns...)
}

func (s *s3ObjectClientStub) UploadPart(ctx context.Context, input *s3.UploadPartInput, optFns ...func(*s3.Options)) (*s3.UploadPartOutput, error) {
	return s.uploadPart(ctx, input, optFns...)
}

func (s *s3ObjectClientStub) CompleteMultipartUpload(ctx context.Context, input *s3.CompleteMultipartUploadInput, optFns ...func(*s3.Options)) (*s3.CompleteMultipartUploadOutput, error) {
	return s.completeMultipartUpload(ctx, input, optFns...)
}

func (s *s3ObjectClientStub) AbortMultipartUpload(ctx context.Context, input *s3.AbortMultipartUploadInput, optFns ...func(*s3.Options)) (*s3.AbortMultipartUploadOutput, error) {
	return s.abortMultipartUpload(ctx, input, optFns...)
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
