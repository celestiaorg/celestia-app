package fibre

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/cosmos/cosmos-sdk/types/bech32"
)

const (
	objectCheckPrefix               = ".fibre-check"
	validatorConsensusAddressPrefix = "celestiavalcons"
	consensusAddressLength          = 20
)

type objectCheckClient interface {
	HeadBucket(context.Context, *s3.HeadBucketInput, ...func(*s3.Options)) (*s3.HeadBucketOutput, error)
	PutObject(context.Context, *s3.PutObjectInput, ...func(*s3.Options)) (*s3.PutObjectOutput, error)
	DeleteObject(context.Context, *s3.DeleteObjectInput, ...func(*s3.Options)) (*s3.DeleteObjectOutput, error)
}

// ObjectStorageCheckResult reports each object storage connectivity check.
type ObjectStorageCheckResult struct {
	CanaryKey         string
	CredentialSource  string
	AccessKeyIDSuffix string
	NamespaceScoped   bool
	HeadBucketErr     error
	PutCanaryErr      error
	DeleteCanaryErr   error
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
	if err := normalizeObjectCheckNamespace(&cfg); err != nil {
		return ObjectStorageCheckResult{}, err
	}
	if err := cfg.Validate(); err != nil {
		return ObjectStorageCheckResult{}, err
	}
	clientCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	client, credentials, err := newS3Client(clientCtx, cfg)
	cancel()
	if err != nil {
		return ObjectStorageCheckResult{}, objectStorageCheckClientError(err)
	}
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return ObjectStorageCheckResult{}, fmt.Errorf("generating canary key: %w", err)
	}
	key := objectCheckKey(cfg, hex.EncodeToString(id[:]))
	result := checkObjectStorage(ctx, client, cfg, key)
	result.CredentialSource = credentials.Source
	if result.CredentialSource == "" {
		result.CredentialSource = "unknown"
	}
	result.AccessKeyIDSuffix = accessKeyIDSuffix(credentials.AccessKeyID)
	result.NamespaceScoped = cfg.ChainID != ""
	return result, nil
}

func objectCheckKey(cfg ObjectStorageConfig, id string) string {
	namespace := cfg.canonical()
	if namespace.ChainID == "" {
		return path.Join(namespace.Prefix, objectCheckPrefix, id)
	}
	return path.Join(namespace.Prefix, namespace.ChainID, namespace.ValidatorAddress, shardsSubdir, objectCheckPrefix, id)
}

func normalizeObjectCheckNamespace(cfg *ObjectStorageConfig) error {
	cfg.ChainID = strings.TrimSpace(cfg.ChainID)
	cfg.ValidatorAddress = strings.TrimSpace(cfg.ValidatorAddress)
	if (cfg.ChainID == "") != (cfg.ValidatorAddress == "") {
		return fmt.Errorf("chain ID and validator address must be provided together")
	}
	if cfg.ChainID == "" {
		return nil
	}
	if cfg.ChainID == "." || cfg.ChainID == ".." || strings.Contains(cfg.ChainID, "/") {
		return fmt.Errorf("chain ID must be a single path segment")
	}
	prefix, address, err := bech32.DecodeAndConvert(cfg.ValidatorAddress)
	if err != nil {
		return fmt.Errorf("invalid validator address: %w", err)
	}
	if prefix != validatorConsensusAddressPrefix || len(address) != consensusAddressLength {
		return fmt.Errorf("validator address must be a %s address", validatorConsensusAddressPrefix)
	}
	canonicalAddress, err := bech32.ConvertAndEncode(prefix, address)
	if err != nil {
		return fmt.Errorf("canonicalizing validator address: %w", err)
	}
	cfg.ValidatorAddress = canonicalAddress
	return nil
}

func objectStorageCheckClientError(err error) error {
	if _, ok := errors.AsType[*objectStorageCredentialsError](err); ok {
		return fmt.Errorf("%w; load the service credential environment before running the check", err)
	}
	return err
}

func accessKeyIDSuffix(accessKeyID string) string {
	const suffixLength = 4
	if len(accessKeyID) <= suffixLength {
		return accessKeyID
	}
	return accessKeyID[len(accessKeyID)-suffixLength:]
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
