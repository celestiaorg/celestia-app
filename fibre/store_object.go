package fibre

import (
	"bufio"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"path"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
	"github.com/celestiaorg/celestia-app/v10/x/fibre/types"
)

const (
	maxObjectDeleteBatchSize     = 1000
	maxMultipartParts            = int64(10_000)
	defaultMultipartAbortTimeout = 10 * time.Second
)

type s3ObjectClient interface {
	PutObject(context.Context, *s3.PutObjectInput, ...func(*s3.Options)) (*s3.PutObjectOutput, error)
	CreateMultipartUpload(context.Context, *s3.CreateMultipartUploadInput, ...func(*s3.Options)) (*s3.CreateMultipartUploadOutput, error)
	UploadPart(context.Context, *s3.UploadPartInput, ...func(*s3.Options)) (*s3.UploadPartOutput, error)
	CompleteMultipartUpload(context.Context, *s3.CompleteMultipartUploadInput, ...func(*s3.Options)) (*s3.CompleteMultipartUploadOutput, error)
	AbortMultipartUpload(context.Context, *s3.AbortMultipartUploadInput, ...func(*s3.Options)) (*s3.AbortMultipartUploadOutput, error)
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
	client                s3ObjectClient
	namespace             objectNamespace
	metrics               *serverMetrics
	requestTimeout        time.Duration
	multipartThreshold    int64
	multipartPartSize     int64
	multipartPartRequests chan struct{}
}

var _ shardBackend = (*objectBackend)(nil)

func (*objectBackend) backendTag() shardBackendTag {
	return objectBackendTag
}

func newObjectBackend(client s3ObjectClient, namespace objectNamespace) *objectBackend {
	b := &objectBackend{
		client:         client,
		namespace:      namespace.canonical(),
		requestTimeout: defaultObjectRequestTimeout,
	}
	b.configureMultipart(defaultMultipartThreshold, defaultMultipartPartSize, defaultMultipartConcurrency)
	return b
}

func (b *objectBackend) configureMultipart(threshold, partSize int64, concurrency int) {
	b.multipartThreshold = threshold
	b.multipartPartSize = partSize
	b.multipartPartRequests = make(chan struct{}, concurrency)
}

func (b *objectBackend) Put(ctx context.Context, commitment Commitment, promiseHash []byte, shard *types.BlobShard) (err error) {
	ctx, cancel := context.WithTimeout(ctx, b.requestTimeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return err
	}

	reader, err := newShardReader(shard)
	if err != nil {
		return fmt.Errorf("encoding shard object: %w", err)
	}

	mode := "single"
	if reader.size >= b.multipartThreshold {
		mode = "multipart"
	}
	done := b.metrics.observeBackendPut(ctx, mode, reader.size)
	defer func() { done(err) }()
	if mode == "multipart" {
		return b.putMultipart(ctx, commitment, promiseHash, reader)
	}
	return b.putObject(ctx, commitment, promiseHash, reader)
}

func (b *objectBackend) putObject(ctx context.Context, commitment Commitment, promiseHash []byte, reader *shardReader) error {
	_, putErr := b.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(b.namespace.Bucket),
		Key:           aws.String(b.objectKey(commitment, promiseHash)),
		Body:          reader,
		ContentLength: aws.Int64(reader.size),
		IfNoneMatch:   aws.String("*"),
	}, objectUploadOptions)

	if hasObjectErrorCode(putErr, "PreconditionFailed") {
		// IfNoneMatch: "*" rejected this upload because a shard already exists for this commitment and promise hash.
		return nil
	}
	if putErr != nil {
		return fmt.Errorf("putting shard object: %w", putErr)
	}
	return nil
}

type multipartPart struct {
	number int32
	offset int64
	size   int64
}

func (b *objectBackend) putMultipart(ctx context.Context, commitment Commitment, promiseHash []byte, reader *shardReader) error {
	partCount := int64(1) + (reader.size-1)/b.multipartPartSize
	if partCount > maxMultipartParts {
		return fmt.Errorf("multipart shard has %d parts, maximum is %d", partCount, maxMultipartParts)
	}
	key := b.objectKey(commitment, promiseHash)
	created, err := b.client.CreateMultipartUpload(ctx, &s3.CreateMultipartUploadInput{
		Bucket: aws.String(b.namespace.Bucket),
		Key:    aws.String(key),
	}, objectUploadOptions)
	if err != nil {
		return fmt.Errorf("creating multipart shard upload: %w", err)
	}
	if created == nil || created.UploadId == nil || *created.UploadId == "" {
		return errors.New("creating multipart shard upload: response has no upload ID")
	}

	parts, err := b.uploadParts(ctx, key, created.UploadId, reader, int(partCount))
	if err != nil {
		abortErr := b.abortMultipartUpload(ctx, key, created.UploadId)
		return errors.Join(fmt.Errorf("uploading shard parts: %w", err), abortErr)
	}

	_, err = b.client.CompleteMultipartUpload(ctx, &s3.CompleteMultipartUploadInput{
		Bucket:      aws.String(b.namespace.Bucket),
		Key:         aws.String(key),
		UploadId:    created.UploadId,
		IfNoneMatch: aws.String("*"),
		MultipartUpload: &s3types.CompletedMultipartUpload{
			Parts: parts,
		},
	}, objectUploadOptions)
	if err == nil {
		return nil
	}
	abortErr := b.abortMultipartUpload(ctx, key, created.UploadId)
	if abortErr != nil {
		return errors.Join(fmt.Errorf("completing multipart shard upload: %w", err), abortErr)
	}
	if hasObjectErrorCode(err, "PreconditionFailed") {
		return nil
	}
	return fmt.Errorf("completing multipart shard upload: %w", err)
}

func (b *objectBackend) uploadParts(ctx context.Context, key string, uploadID *string, reader *shardReader, partCount int) ([]s3types.CompletedPart, error) {
	uploadCtx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)

	parts := make([]s3types.CompletedPart, partCount)
	jobs := make(chan multipartPart)
	workerCount := min(partCount, cap(b.multipartPartRequests))
	var (
		workers  sync.WaitGroup
		errOnce  sync.Once
		firstErr error
	)
	setError := func(err error) {
		if err == nil {
			return
		}
		errOnce.Do(func() {
			firstErr = err
			cancel(err)
		})
	}

	workers.Add(workerCount)
	for range workerCount {
		go func() {
			defer workers.Done()
			for part := range jobs {
				completed, err := b.uploadPart(uploadCtx, key, uploadID, reader, part)
				if err != nil {
					setError(err)
					continue
				}
				parts[part.number-1] = completed
			}
		}()
	}

sendParts:
	for i := range partCount {
		offset := int64(i) * b.multipartPartSize
		part := multipartPart{
			number: int32(i + 1),
			offset: offset,
			size:   min(b.multipartPartSize, reader.size-offset),
		}
		select {
		case jobs <- part:
		case <-uploadCtx.Done():
			setError(context.Cause(uploadCtx))
			break sendParts
		}
	}
	close(jobs)
	workers.Wait()
	return parts, firstErr
}

func (b *objectBackend) uploadPart(ctx context.Context, key string, uploadID *string, reader *shardReader, part multipartPart) (_ s3types.CompletedPart, err error) {
	select {
	case b.multipartPartRequests <- struct{}{}:
		defer func() { <-b.multipartPartRequests }()
	case <-ctx.Done():
		return s3types.CompletedPart{}, context.Cause(ctx)
	}
	done := b.metrics.observeMultipartPart(ctx)
	defer func() { done(err) }()

	body := io.NewSectionReader(reader, part.offset, part.size)
	output, err := b.client.UploadPart(ctx, &s3.UploadPartInput{
		Bucket:        aws.String(b.namespace.Bucket),
		Key:           aws.String(key),
		UploadId:      uploadID,
		PartNumber:    aws.Int32(part.number),
		Body:          body,
		ContentLength: aws.Int64(part.size),
	}, objectUploadOptions)
	if err != nil {
		return s3types.CompletedPart{}, fmt.Errorf("uploading part %d: %w", part.number, err)
	}
	if output == nil || output.ETag == nil || *output.ETag == "" {
		return s3types.CompletedPart{}, fmt.Errorf("uploading part %d: response has no ETag", part.number)
	}
	return s3types.CompletedPart{ETag: output.ETag, PartNumber: aws.Int32(part.number)}, nil
}

func (b *objectBackend) abortMultipartUpload(ctx context.Context, key string, uploadID *string) error {
	abortCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), defaultMultipartAbortTimeout)
	defer cancel()
	_, err := b.client.AbortMultipartUpload(abortCtx, &s3.AbortMultipartUploadInput{
		Bucket:   aws.String(b.namespace.Bucket),
		Key:      aws.String(key),
		UploadId: uploadID,
	}, objectUploadOptions)
	b.metrics.observeMultipartAbort(abortCtx, err)
	if err != nil {
		return fmt.Errorf("aborting multipart shard upload: %w", err)
	}
	return nil
}

func objectUploadOptions(options *s3.Options) {
	options.RequestChecksumCalculation = aws.RequestChecksumCalculationWhenRequired
	// Avoid hashing shard payloads for SigV4 signing.
	options.APIOptions = append(options.APIOptions, v4.SwapComputePayloadSHA256ForUnsignedPayloadMiddleware)
}

func (b *objectBackend) Get(ctx context.Context, commitment Commitment, promiseHash []byte) (_ *types.BlobShard, err error) {
	ctx, cancel := context.WithTimeout(ctx, b.requestTimeout)
	defer cancel()
	done := b.metrics.observeBackendGet(ctx, storageBackendObject)
	defer func() { done(err) }()
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	output, err := b.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(b.namespace.Bucket),
		Key:    aws.String(b.objectKey(commitment, promiseHash)),
	})
	if isObjectNotFound(err) {
		return nil, ErrStoreNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("getting shard object: %w", err)
	}
	defer output.Body.Close()

	reader := bufio.NewReaderSize(b.metrics.backendReader(ctx, storageBackendObject, output.Body), 1<<20)
	shard, err := readShardBinary(reader)
	if err != nil {
		return nil, fmt.Errorf("decoding shard object: %w", err)
	}
	if _, err := reader.ReadByte(); err != io.EOF {
		if err != nil {
			return nil, fmt.Errorf("reading shard object: %w", err)
		}
		return nil, errors.New("unexpected trailing data in shard object")
	}
	return shard, nil
}

func (b *objectBackend) Has(ctx context.Context, commitment Commitment, promiseHash []byte) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, b.requestTimeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return false, err
	}

	_, err := b.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(b.namespace.Bucket),
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
	ctx, cancel := context.WithTimeout(ctx, b.requestTimeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return err
	}

	_, err := b.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(b.namespace.Bucket),
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
	ctx, cancel := context.WithTimeout(ctx, b.requestTimeout)
	defer cancel()
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
	// S3 returns failures by key; map each key back to its input positions.
	// Keep every position so duplicate IDs receive the same error.
	indices := make(map[string][]int, len(ids))
	for i, id := range ids {
		key := b.objectKey(id.commitment, id.promiseHash)
		objects[i].Key = aws.String(key)
		indices[key] = append(indices[key], i)
	}

	output, err := b.client.DeleteObjects(ctx, &s3.DeleteObjectsInput{
		Bucket: aws.String(b.namespace.Bucket),
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
		b.namespace.Prefix,
		b.namespace.ChainID,
		b.namespace.ValidatorAddress,
		shardsSubdir,
		commitment.String()+"-"+hex.EncodeToString(promiseHash),
	)
}

func isObjectNotFound(err error) bool {
	return hasObjectErrorCode(err, "NoSuchKey") || hasObjectErrorCode(err, "NotFound")
}

// hasObjectErrorCode unwraps AWS SDK service errors and matches their API code.
func hasObjectErrorCode(err error, code string) bool {
	var apiErr smithy.APIError
	return errors.As(err, &apiErr) && apiErr.ErrorCode() == code
}
