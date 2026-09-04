package fibre

import (
	"bufio"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
	"github.com/celestiaorg/celestia-app/v10/x/fibre/types"
)

const maxObjectDeleteBatchSize = 1000

type s3ObjectClient interface {
	PutObject(context.Context, *s3.PutObjectInput, ...func(*s3.Options)) (*s3.PutObjectOutput, error)
	GetObject(context.Context, *s3.GetObjectInput, ...func(*s3.Options)) (*s3.GetObjectOutput, error)
	HeadObject(context.Context, *s3.HeadObjectInput, ...func(*s3.Options)) (*s3.HeadObjectOutput, error)
	DeleteObject(context.Context, *s3.DeleteObjectInput, ...func(*s3.Options)) (*s3.DeleteObjectOutput, error)
	DeleteObjects(context.Context, *s3.DeleteObjectsInput, ...func(*s3.Options)) (*s3.DeleteObjectsOutput, error)
}

type shardID struct {
	commitment  Commitment
	promiseHash []byte
}

// objectBackend stores shard payloads in S3-compatible object storage.
type objectBackend struct {
	client           s3ObjectClient
	bucket           string
	prefix           string
	chainID          string
	validatorAddress string
}

var _ shardBackend = (*objectBackend)(nil)

func (*objectBackend) backendTag() shardBackendTag {
	return objectBackendTag
}

func newObjectBackend(client s3ObjectClient, bucket, prefix, chainID, validatorAddress string) *objectBackend {
	return &objectBackend{
		client:           client,
		bucket:           bucket,
		prefix:           strings.Trim(prefix, "/"),
		chainID:          chainID,
		validatorAddress: validatorAddress,
	}
}

func (b *objectBackend) Put(ctx context.Context, commitment Commitment, promiseHash []byte, shard *types.BlobShard) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}

	reader, writer := io.Pipe()
	writeDone := make(chan error, 1)
	go func() {
		err := writeShardBinary(writer, shard)
		_ = writer.CloseWithError(err)
		writeDone <- err
	}()

	_, putErr := b.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(b.bucket),
		Key:           aws.String(b.objectKey(commitment, promiseHash)),
		Body:          reader,
		ContentLength: aws.Int64(shardBinarySize(shard)),
		IfNoneMatch:   aws.String("*"),
	}, func(options *s3.Options) {
		options.RequestChecksumCalculation = aws.RequestChecksumCalculationWhenRequired
	})
	_ = reader.CloseWithError(putErr)
	writeErr := <-writeDone

	if hasObjectErrorCode(putErr, "PreconditionFailed") {
		return false, nil
	}
	if putErr != nil {
		return false, fmt.Errorf("putting shard object: %w", putErr)
	}
	if writeErr != nil {
		return false, fmt.Errorf("encoding shard object: %w", writeErr)
	}
	return true, nil
}

func (b *objectBackend) Get(ctx context.Context, commitment Commitment, promiseHash []byte) (*types.BlobShard, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	output, err := b.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(b.bucket),
		Key:    aws.String(b.objectKey(commitment, promiseHash)),
	})
	if isObjectNotFound(err) {
		return nil, ErrStoreNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("getting shard object: %w", err)
	}
	defer output.Body.Close()

	shard, err := readShardBinary(bufio.NewReaderSize(output.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("decoding shard object: %w", err)
	}
	return shard, nil
}

func (b *objectBackend) Has(ctx context.Context, commitment Commitment, promiseHash []byte) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}

	_, err := b.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(b.bucket),
		Key:    aws.String(b.objectKey(commitment, promiseHash)),
	})
	switch {
	case isObjectNotFound(err):
		return false, nil
	case err != nil:
		return false, fmt.Errorf("checking shard object: %w", err)
	default:
		return true, nil
	}
}

func (b *objectBackend) Delete(ctx context.Context, commitment Commitment, promiseHash []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	_, err := b.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(b.bucket),
		Key:    aws.String(b.objectKey(commitment, promiseHash)),
	})
	if isObjectNotFound(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("deleting shard object: %w", err)
	}
	return nil
}

// DeleteObjects deletes up to 1,000 objects. Returned errors align with ids.
func (b *objectBackend) DeleteObjects(ctx context.Context, ids []shardID) ([]error, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(ids) > maxObjectDeleteBatchSize {
		return nil, fmt.Errorf("object delete batch has %d entries, maximum is %d", len(ids), maxObjectDeleteBatchSize)
	}
	if len(ids) == 0 {
		return []error{}, nil
	}

	objects := make([]s3types.ObjectIdentifier, len(ids))
	indices := make(map[string][]int, len(ids))
	for i, id := range ids {
		key := b.objectKey(id.commitment, id.promiseHash)
		objects[i].Key = aws.String(key)
		indices[key] = append(indices[key], i)
	}

	output, err := b.client.DeleteObjects(ctx, &s3.DeleteObjectsInput{
		Bucket: aws.String(b.bucket),
		Delete: &s3types.Delete{
			Objects: objects,
			Quiet:   aws.Bool(true),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("deleting shard objects: %w", err)
	}

	result := make([]error, len(ids))
	for _, objectErr := range output.Errors {
		if objectErr.Key == nil {
			return nil, errors.New("object delete response contains an error without a key")
		}
		key := *objectErr.Key
		keyIndices, ok := indices[key]
		if !ok {
			return nil, fmt.Errorf("object delete response contains unknown key %q", key)
		}
		code := aws.ToString(objectErr.Code)
		if code == "NoSuchKey" || code == "NotFound" {
			continue
		}
		deleteErr := fmt.Errorf("deleting object %q: %s: %s", key, code, aws.ToString(objectErr.Message))
		for _, i := range keyIndices {
			result[i] = deleteErr
		}
	}
	return result, nil
}

func (b *objectBackend) objectKey(commitment Commitment, promiseHash []byte) string {
	return path.Join(
		b.prefix,
		b.chainID,
		b.validatorAddress,
		shardsSubdir,
		commitment.String()+"-"+hex.EncodeToString(promiseHash),
	)
}

func isObjectNotFound(err error) bool {
	return hasObjectErrorCode(err, "NoSuchKey") || hasObjectErrorCode(err, "NotFound")
}

func hasObjectErrorCode(err error, code string) bool {
	var apiErr smithy.APIError
	return errors.As(err, &apiErr) && apiErr.ErrorCode() == code
}
