package fibre

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/celestiaorg/celestia-app/v8/x/fibre/types"
	pebbledb "github.com/cockroachdb/pebble/v2"
)

const (
	// promiseKeyPrefix is the datastore key prefix for payment promises.
	promiseKeyPrefix = "/pp/"
	// shardKeyPrefix is the datastore key prefix for blob shards.
	shardKeyPrefix = "/shard/"
	// pruneKeyPrefix is the datastore key prefix for prune index entries.
	pruneKeyPrefix = "/prune/"

	// timestampLayout is the time format used for prune index keys (YYYYMMDDHHmm).
	timestampLayout = "200601021504"
	// timestampLen is the length of a formatted timestamp string.
	timestampLen = len(timestampLayout)

	// commitmentHexLen is the hex-encoded length of a Commitment.
	commitmentHexLen = CommitmentSize * 2

	// pebbleBatchHeader is the fixed header size of a pebble batch (sequence number + count).
	pebbleBatchHeader = 12
)

// putPayload holds pre-computed sizes and references needed to write
// a single payment promise + shard + prune-index entry into a pebble batch
// without intermediate allocations. Concrete types are used instead of
// interfaces to enable devirtualization of Size/MarshalToSizedBuffer calls.
type putPayload struct {
	promiseHash  []byte
	commitment   Commitment
	pruneAt      time.Time
	promiseProto *types.PaymentPromise
	ppSize       int
	shard        *types.BlobShard
	shardSize    int
}

// sizedMarshaler is implemented by gogoproto-generated types that support
// zero-copy marshaling into a pre-allocated buffer.
type sizedMarshaler interface {
	Size() int
	MarshalToSizedBuffer([]byte) (int, error)
}

// applyPebble writes the promise, shard, and prune-index entry into a pebble batch
// using SetDeferred to avoid extra allocations.
func (p *putPayload) applyPebble(batch *pebbledb.Batch) error {
	if err := writePebblePromisePayload(batch, p.promiseHash, p.ppSize, p.promiseProto); err != nil {
		return fmt.Errorf("writing payment promise: %w", err)
	}
	if err := writePebbleShardPayload(batch, p.commitment, p.promiseHash, p.shardSize, p.shard); err != nil {
		return fmt.Errorf("writing shard: %w", err)
	}
	if err := writePebblePrunePayload(batch, p.pruneAt, p.commitment, p.promiseHash); err != nil {
		return fmt.Errorf("writing prune index: %w", err)
	}
	return nil
}

func writePebblePromisePayload(batch *pebbledb.Batch, promiseHash []byte, valueSize int, value sizedMarshaler) error {
	op := batch.SetDeferred(promiseKeyLen(promiseHash), valueSize)
	n := copy(op.Key, promiseKeyPrefix)
	hex.Encode(op.Key[n:], promiseHash)
	if err := marshalToSizedBuffer(op.Value, value); err != nil {
		return err
	}
	if err := op.Finish(); err != nil {
		return fmt.Errorf("finishing pebble batch op: %w", err)
	}
	return nil
}

func writePebbleShardPayload(batch *pebbledb.Batch, commitment Commitment, promiseHash []byte, valueSize int, value sizedMarshaler) error {
	op := batch.SetDeferred(shardKeyLen(promiseHash), valueSize)
	n := copy(op.Key, shardKeyPrefix)
	hex.Encode(op.Key[n:n+commitmentHexLen], commitment[:])
	n += commitmentHexLen
	op.Key[n] = '/'
	n++
	hex.Encode(op.Key[n:], promiseHash)
	if err := marshalToSizedBuffer(op.Value, value); err != nil {
		return err
	}
	if err := op.Finish(); err != nil {
		return fmt.Errorf("finishing pebble batch op: %w", err)
	}
	return nil
}

func writePebblePrunePayload(batch *pebbledb.Batch, pruneAt time.Time, commitment Commitment, promiseHash []byte) error {
	op := batch.SetDeferred(pruneKeyLen(promiseHash), 0)
	n := copy(op.Key, pruneKeyPrefix)
	key := pruneAt.UTC().AppendFormat(op.Key[:n], timestampLayout)
	n = len(key)
	op.Key[n] = '/'
	n++
	hex.Encode(op.Key[n:n+commitmentHexLen], commitment[:])
	n += commitmentHexLen
	op.Key[n] = '/'
	n++
	hex.Encode(op.Key[n:], promiseHash)
	if err := op.Finish(); err != nil {
		return fmt.Errorf("finishing pebble batch op: %w", err)
	}
	return nil
}

// marshalToSizedBuffer marshals a proto message into a pre-allocated buffer,
// verifying that the written size matches the buffer length.
func marshalToSizedBuffer(dst []byte, m sizedMarshaler) error {
	n, err := m.MarshalToSizedBuffer(dst)
	if err != nil {
		return err
	}
	if n != len(dst) {
		return fmt.Errorf("marshal size mismatch: wrote %d bytes into %d-byte buffer", n, len(dst))
	}
	return nil
}

// pebblePayloadsBatchSize returns the total batch buffer size needed
// to hold all payloads, including the pebble batch header.
func pebblePayloadsBatchSize(payloads []*putPayload) int {
	size := pebbleBatchHeader
	for _, payload := range payloads {
		size += pebblePayloadBatchSize(payload)
	}
	return size
}

// pebblePayloadBatchSize returns the batch buffer size for a single payload
// (3 entries: promise + shard + prune index).
func pebblePayloadBatchSize(payload *putPayload) int {
	return pebbleBatchEntrySize(promiseKeyLen(payload.promiseHash), payload.ppSize) +
		pebbleBatchEntrySize(shardKeyLen(payload.promiseHash), payload.shardSize) +
		pebbleBatchEntrySize(pruneKeyLen(payload.promiseHash), 0)
}

// pebbleBatchEntrySize returns the size of a single pebble batch entry:
// 1 byte kind tag + up to 2 varint-encoded lengths + key + value.
func pebbleBatchEntrySize(keyLen, valueLen int) int {
	return 1 + 2*binary.MaxVarintLen32 + keyLen + valueLen
}

func promiseKeyLen(promiseHash []byte) int {
	return len(promiseKeyPrefix) + hex.EncodedLen(len(promiseHash))
}

func shardKeyLen(promiseHash []byte) int {
	return len(shardKeyPrefix) + commitmentHexLen + 1 + hex.EncodedLen(len(promiseHash))
}

func pruneKeyLen(promiseHash []byte) int {
	return len(pruneKeyPrefix) + timestampLen + 1 + commitmentHexLen + 1 + hex.EncodedLen(len(promiseHash))
}
