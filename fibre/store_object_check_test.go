package fibre

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/stretchr/testify/require"
)

func TestCheckObjectStorageOperations(t *testing.T) {
	headErr := errors.New("head denied")
	var deleted bool
	client := &objectCheckClientStub{
		headBucket: func(_ context.Context, input *s3.HeadBucketInput, _ ...func(*s3.Options)) (*s3.HeadBucketOutput, error) {
			require.Equal(t, "bucket", aws.ToString(input.Bucket))
			return nil, headErr
		},
		putObject: func(_ context.Context, input *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
			require.Equal(t, "bucket", aws.ToString(input.Bucket))
			require.Equal(t, "prefix/.fibre-check/canary", aws.ToString(input.Key))
			require.Equal(t, "*", aws.ToString(input.IfNoneMatch))
			body, err := io.ReadAll(input.Body)
			require.NoError(t, err)
			require.Empty(t, body)
			return &s3.PutObjectOutput{}, nil
		},
		deleteObject: func(_ context.Context, input *s3.DeleteObjectInput, _ ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
			deleted = true
			require.Equal(t, "bucket", aws.ToString(input.Bucket))
			require.Equal(t, "prefix/.fibre-check/canary", aws.ToString(input.Key))
			return &s3.DeleteObjectOutput{}, nil
		},
	}
	cfg := testObjectStorageConfig()
	cfg.Bucket = "bucket"
	cfg.Prefix = "prefix"
	result := checkObjectStorage(t.Context(), client, cfg, "prefix/.fibre-check/canary")

	require.ErrorIs(t, result.HeadBucketErr, headErr)
	require.NoError(t, result.PutCanaryErr)
	require.NoError(t, result.DeleteCanaryErr)
	require.True(t, deleted)
	require.ErrorContains(t, result.Err(), "head bucket")
}

func TestObjectCheckKeyUsesCanonicalPrefix(t *testing.T) {
	cfg := testObjectStorageConfig()
	cfg.Prefix = "/fibre/"
	require.Equal(t, "fibre/.fibre-check/canary", objectCheckKey(cfg, "canary"))
}

func TestCheckObjectStorageUsesShardPutOptions(t *testing.T) {
	client := &objectCheckClientStub{
		putObject: func(_ context.Context, _ *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
			options := s3.Options{}
			for _, optFn := range optFns {
				optFn(&options)
			}
			require.Equal(t, aws.RequestChecksumCalculationWhenRequired, options.RequestChecksumCalculation)
			require.NotEmpty(t, options.APIOptions)
			return &s3.PutObjectOutput{}, nil
		},
	}
	checkObjectStorage(t.Context(), client, testObjectStorageConfig(), "fibre/.fibre-check/canary")
}

func TestCheckObjectStorageDeletesAfterPutFailure(t *testing.T) {
	putErr := errors.New("put denied")
	var deleted bool
	client := &objectCheckClientStub{
		putObject: func(context.Context, *s3.PutObjectInput, ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
			return nil, putErr
		},
		deleteObject: func(context.Context, *s3.DeleteObjectInput, ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
			deleted = true
			return &s3.DeleteObjectOutput{}, nil
		},
	}
	result := checkObjectStorage(t.Context(), client, testObjectStorageConfig(), "fibre/.fibre-check/canary")

	require.ErrorIs(t, result.PutCanaryErr, putErr)
	require.NoError(t, result.DeleteCanaryErr)
	require.True(t, deleted)
	require.ErrorIs(t, result.Err(), putErr)
}

func TestCheckObjectStorageReportsDeleteFailureAndKey(t *testing.T) {
	deleteErr := errors.New("delete denied")
	client := &objectCheckClientStub{
		deleteObject: func(context.Context, *s3.DeleteObjectInput, ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
			return nil, deleteErr
		},
	}
	key := "fibre/.fibre-check/manual-cleanup"
	result := checkObjectStorage(t.Context(), client, testObjectStorageConfig(), key)

	require.ErrorIs(t, result.DeleteCanaryErr, deleteErr)
	require.ErrorContains(t, result.Err(), key)
}

type objectCheckClientStub struct {
	headBucket   func(context.Context, *s3.HeadBucketInput, ...func(*s3.Options)) (*s3.HeadBucketOutput, error)
	putObject    func(context.Context, *s3.PutObjectInput, ...func(*s3.Options)) (*s3.PutObjectOutput, error)
	deleteObject func(context.Context, *s3.DeleteObjectInput, ...func(*s3.Options)) (*s3.DeleteObjectOutput, error)
}

func (s *objectCheckClientStub) HeadBucket(ctx context.Context, input *s3.HeadBucketInput, opts ...func(*s3.Options)) (*s3.HeadBucketOutput, error) {
	if s.headBucket == nil {
		return &s3.HeadBucketOutput{}, nil
	}
	return s.headBucket(ctx, input, opts...)
}

func (s *objectCheckClientStub) PutObject(ctx context.Context, input *s3.PutObjectInput, opts ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	if s.putObject == nil {
		return &s3.PutObjectOutput{}, nil
	}
	return s.putObject(ctx, input, opts...)
}

func (s *objectCheckClientStub) DeleteObject(ctx context.Context, input *s3.DeleteObjectInput, opts ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
	if s.deleteObject == nil {
		return &s3.DeleteObjectOutput{}, nil
	}
	return s.deleteObject(ctx, input, opts...)
}

func TestObjectStorageCheckResultJoinsErrors(t *testing.T) {
	result := ObjectStorageCheckResult{
		CanaryKey:       "prefix/.fibre-check/canary",
		HeadBucketErr:   errors.New("head failed"),
		PutCanaryErr:    errors.New("put failed"),
		DeleteCanaryErr: errors.New("delete failed"),
	}
	err := result.Err()
	require.Error(t, err)
	for _, message := range []string{"head bucket", "put canary", "delete canary", result.CanaryKey} {
		require.True(t, strings.Contains(err.Error(), message))
	}
}
