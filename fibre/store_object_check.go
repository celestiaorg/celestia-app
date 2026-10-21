package fibre

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"path"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

const objectCheckPrefix = ".fibre-check"

type objectCheckClient interface {
	HeadBucket(context.Context, *s3.HeadBucketInput, ...func(*s3.Options)) (*s3.HeadBucketOutput, error)
	PutObject(context.Context, *s3.PutObjectInput, ...func(*s3.Options)) (*s3.PutObjectOutput, error)
	DeleteObject(context.Context, *s3.DeleteObjectInput, ...func(*s3.Options)) (*s3.DeleteObjectOutput, error)
}

// ObjectStorageCheckResult reports each object storage connectivity check.
type ObjectStorageCheckResult struct {
	CanaryKey       string
	HeadBucketErr   error
	PutCanaryErr    error
	DeleteCanaryErr error
}

// Err returns all failed checks.
func (r ObjectStorageCheckResult) Err() error {
	var err error
	if r.HeadBucketErr != nil {
		err = errors.Join(err, fmt.Errorf("head bucket: %w", r.HeadBucketErr))
	}
	if r.PutCanaryErr != nil {
		err = errors.Join(err, fmt.Errorf("put canary: %w", r.PutCanaryErr))
	}
	if r.DeleteCanaryErr != nil {
		err = errors.Join(err, fmt.Errorf("delete canary %q: %w", r.CanaryKey, r.DeleteCanaryErr))
	}
	return err
}

// CheckObjectStorage checks access to the configured bucket with a temporary object.
func CheckObjectStorage(ctx context.Context, cfg ObjectStorageConfig) (ObjectStorageCheckResult, error) {
	if err := cfg.Validate(); err != nil {
		return ObjectStorageCheckResult{}, err
	}
	clientCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	client, err := newS3Client(clientCtx, cfg)
	cancel()
	if err != nil {
		return ObjectStorageCheckResult{}, err
	}
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return ObjectStorageCheckResult{}, fmt.Errorf("generating canary key: %w", err)
	}
	key := objectCheckKey(cfg, hex.EncodeToString(id[:]))
	return checkObjectStorage(ctx, client, cfg, key), nil
}

func objectCheckKey(cfg ObjectStorageConfig, id string) string {
	return path.Join(cfg.canonical().Prefix, objectCheckPrefix, id)
}

func checkObjectStorage(ctx context.Context, client objectCheckClient, cfg ObjectStorageConfig, key string) ObjectStorageCheckResult {
	result := ObjectStorageCheckResult{CanaryKey: key}
	headCtx, cancel := context.WithTimeout(ctx, cfg.RequestTimeout)
	_, result.HeadBucketErr = client.HeadBucket(headCtx, &s3.HeadBucketInput{Bucket: aws.String(cfg.Bucket)})
	cancel()

	putCtx, cancel := context.WithTimeout(ctx, cfg.RequestTimeout)
	_, result.PutCanaryErr = client.PutObject(putCtx, &s3.PutObjectInput{
		Bucket:        aws.String(cfg.Bucket),
		Key:           aws.String(key),
		Body:          bytes.NewReader(nil),
		ContentLength: aws.Int64(0),
		IfNoneMatch:   aws.String("*"),
	}, objectPutOptions)
	cancel()
	deleteCtx, cancel := context.WithTimeout(ctx, cfg.RequestTimeout)
	_, result.DeleteCanaryErr = client.DeleteObject(deleteCtx, &s3.DeleteObjectInput{
		Bucket: aws.String(cfg.Bucket),
		Key:    aws.String(key),
	})
	cancel()
	return result
}
