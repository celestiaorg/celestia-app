package migrate

import (
	"bytes"
	"context"
	"fmt"

	cosmosdb "github.com/cosmos/cosmos-db"
	"golang.org/x/time/rate"
)

const (
	// ChunkBytes is the amount of data copied per iterator chunk. Chunking prevents
	// long-lived iterators from blocking LevelDB compaction.
	ChunkBytes int64 = 1024 * 1024 * 1024 // 1 GB

	// DefaultBatchBytes is the max batch size before flushing to destination.
	DefaultBatchBytes int64 = 64 * 1024 * 1024 // 64 MB

	// MaxDeleteBatch is the maximum size of a single delete batch.
	MaxDeleteBatch = 64 * 1024 * 1024 // 64 MB
)

// CopyResult holds the outcome of a CopyDB call.
type CopyResult struct {
	KeysCopied  int64
	BytesCopied int64
}

// CopyDBOptions configures a CopyDB call.
type CopyDBOptions struct {
	// BatchBytes is the max batch size before flushing. Defaults to DefaultBatchBytes.
	BatchBytes int64
	// SyncIntervalBytes triggers a WriteSync every N bytes. 0 means sync only at the end.
	SyncIntervalBytes int64
	// Limiter is an optional rate limiter for writes.
	Limiter *rate.Limiter
	// ProgressFn is called periodically with (keysCopied, bytesCopied). Optional.
	ProgressFn func(keys, bytes int64)
}

// CopyDB copies all keys from srcDB to destDB using chunked iteration with resume support.
// It finds the resume point from the last key in destDB, then copies in chunks of ChunkBytes
// to allow LevelDB compaction between chunks.
func CopyDB(ctx context.Context, srcDB, destDB cosmosdb.DB, opts CopyDBOptions) (CopyResult, error) {
	if opts.BatchBytes <= 0 {
		opts.BatchBytes = DefaultBatchBytes
	}

	resumeKey, resumedKeys, err := FindResumePoint(destDB)
	if err != nil {
		return CopyResult{}, err
	}

	totalKeys := resumedKeys
	var totalBytes int64
	lastKey := resumeKey

	for {
		if err := ctx.Err(); err != nil {
			return CopyResult{KeysCopied: totalKeys, BytesCopied: totalBytes}, err
		}

		chunk, err := copyChunk(ctx, srcDB, destDB, lastKey, opts)
		if err != nil {
			return CopyResult{KeysCopied: totalKeys, BytesCopied: totalBytes}, err
		}

		totalKeys += chunk.keys
		totalBytes += chunk.bytes

		if opts.ProgressFn != nil {
			opts.ProgressFn(totalKeys, totalBytes)
		}

		if chunk.lastKey == nil {
			break
		}
		lastKey = chunk.lastKey
	}

	return CopyResult{KeysCopied: totalKeys, BytesCopied: totalBytes}, nil
}

// FindResumePoint finds the last key in destDB and counts existing keys.
// Returns (nil, 0, nil) if the destination is empty.
func FindResumePoint(destDB cosmosdb.DB) (resumeKey []byte, count int64, err error) {
	revIter, err := destDB.ReverseIterator(nil, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to create reverse iterator: %w", err)
	}
	if revIter.Valid() {
		resumeKey = make([]byte, len(revIter.Key()))
		copy(resumeKey, revIter.Key())
	}
	if err := revIter.Close(); err != nil {
		return nil, 0, fmt.Errorf("failed to close reverse iterator: %w", err)
	}

	if resumeKey == nil {
		return nil, 0, nil
	}

	countIter, err := destDB.Iterator(nil, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to count existing keys: %w", err)
	}
	for ; countIter.Valid(); countIter.Next() {
		count++
	}
	countIter.Close()

	return resumeKey, count, nil
}

// IteratorFrom creates a source iterator starting after lastKey.
// If lastKey is nil, iterates from the beginning.
func IteratorFrom(srcDB cosmosdb.DB, lastKey []byte) (cosmosdb.Iterator, error) {
	if lastKey != nil {
		iter, err := srcDB.Iterator(lastKey, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create source iterator: %w", err)
		}
		// Skip the resume key itself (already copied)
		if iter.Valid() && bytes.Equal(iter.Key(), lastKey) {
			iter.Next()
		}
		return iter, nil
	}
	iter, err := srcDB.Iterator(nil, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create source iterator: %w", err)
	}
	return iter, nil
}

// FlushBatch writes the batch and returns a new empty batch.
// If doSync is true, uses WriteSync for durability.
func FlushBatch(batch cosmosdb.Batch, destDB cosmosdb.DB, doSync bool) (cosmosdb.Batch, error) {
	var writeErr error
	if doSync {
		writeErr = batch.WriteSync()
	} else {
		writeErr = batch.Write()
	}
	if writeErr != nil {
		batch.Close()
		return nil, fmt.Errorf("failed to write batch: %w", writeErr)
	}
	batch.Close()
	return destDB.NewBatch(), nil
}

// DeleteSourceKeys deletes keys from sourceDB in sub-batches of MaxDeleteBatch.
func DeleteSourceKeys(sourceDB cosmosdb.DB, keys [][]byte) error {
	batch := sourceDB.NewBatch()
	for _, key := range keys {
		if err := batch.Delete(key); err != nil {
			batch.Close()
			return err
		}
		size, _ := batch.GetByteSize()
		if size >= MaxDeleteBatch {
			if err := batch.WriteSync(); err != nil {
				batch.Close()
				return err
			}
			batch.Close()
			batch = sourceDB.NewBatch()
		}
	}
	if err := batch.WriteSync(); err != nil {
		batch.Close()
		return err
	}
	batch.Close()
	return nil
}

// chunkResult holds the outcome of a single chunk copy.
type chunkResult struct {
	keys    int64
	bytes   int64
	lastKey []byte // nil if source is exhausted
}

// kvBatch is a pre-read set of key-value pairs sent from reader to writer.
type kvBatch struct {
	keys   [][]byte
	values [][]byte
	bytes  int64
}

// copyChunk copies up to ChunkBytes from srcDB starting after lastKey.
// It uses a pipeline: a reader goroutine fills kvBatch structs and sends them
// on a buffered channel, while a writer goroutine flushes them to destDB.
func copyChunk(ctx context.Context, srcDB, destDB cosmosdb.DB, lastKey []byte, opts CopyDBOptions) (chunkResult, error) {
	srcIter, err := IteratorFrom(srcDB, lastKey)
	if err != nil {
		return chunkResult{}, err
	}

	const chanBuf = 2
	batchCh := make(chan kvBatch, chanBuf)

	// --- Reader goroutine ---
	var readerKeys int64
	var readerBytes int64
	var readerLastKey []byte
	var readerErr error
	var sourceExhausted bool

	go func() {
		defer close(batchCh)
		defer srcIter.Close()

		var curBatch kvBatch

		for ; srcIter.Valid(); srcIter.Next() {
			if readerKeys%10000 == 0 {
				if err := ctx.Err(); err != nil {
					readerErr = err
					return
				}
			}

			key := srcIter.Key()
			value := srcIter.Value()
			kvSize := int64(len(key) + len(value))

			// Copy key and value since the iterator reuses buffers.
			keyCopy := make([]byte, len(key))
			copy(keyCopy, key)
			valCopy := make([]byte, len(value))
			copy(valCopy, value)

			curBatch.keys = append(curBatch.keys, keyCopy)
			curBatch.values = append(curBatch.values, valCopy)
			curBatch.bytes += kvSize

			readerKeys++
			readerBytes += kvSize

			if curBatch.bytes >= opts.BatchBytes {
				// Only copy currentKey at batch boundaries, not every key.
				readerLastKey = keyCopy

				select {
				case batchCh <- curBatch:
				case <-ctx.Done():
					readerErr = ctx.Err()
					return
				}
				curBatch = kvBatch{}
			}

			if readerBytes >= ChunkBytes {
				// Send any partial batch before stopping.
				if curBatch.bytes > 0 {
					readerLastKey = curBatch.keys[len(curBatch.keys)-1]
					select {
					case batchCh <- curBatch:
					case <-ctx.Done():
						readerErr = ctx.Err()
						return
					}
				}
				return
			}
		}

		if iterErr := srcIter.Error(); iterErr != nil {
			readerErr = fmt.Errorf("iterator error: %w", iterErr)
			return
		}

		sourceExhausted = true

		// Send any remaining partial batch.
		if curBatch.bytes > 0 {
			readerLastKey = curBatch.keys[len(curBatch.keys)-1]
			select {
			case batchCh <- curBatch:
			case <-ctx.Done():
				readerErr = ctx.Err()
				return
			}
		}
	}()

	// --- Writer (runs on calling goroutine) ---
	var writerErr error
	var writtenKeys int64
	var writtenBytes int64
	var bytesSinceSync int64

	for kb := range batchCh {
		batch := destDB.NewBatch()
		for i := range kb.keys {
			if err := batch.Set(kb.keys[i], kb.values[i]); err != nil {
				batch.Close()
				writerErr = fmt.Errorf("failed to set key in batch: %w", err)
				// Drain channel so reader goroutine can exit.
				for range batchCh {
				}
				goto done
			}
		}

		bytesSinceSync += kb.bytes
		needSync := opts.SyncIntervalBytes > 0 && bytesSinceSync >= opts.SyncIntervalBytes

		if opts.Limiter != nil {
			if err := opts.Limiter.WaitN(ctx, int(kb.bytes)); err != nil {
				batch.Close()
				writerErr = err
				for range batchCh {
				}
				goto done
			}
		}

		if _, err := FlushBatch(batch, destDB, needSync); err != nil {
			writerErr = err
			for range batchCh {
			}
			goto done
		}
		if needSync {
			bytesSinceSync = 0
		}

		writtenKeys += int64(len(kb.keys))
		writtenBytes += kb.bytes
	}

done:
	if writerErr != nil {
		return chunkResult{}, writerErr
	}
	if readerErr != nil {
		return chunkResult{}, readerErr
	}

	// Final sync for any data not yet synced.
	if bytesSinceSync > 0 {
		syncBatch := destDB.NewBatch()
		// Empty batch with WriteSync to force a sync.
		if _, err := FlushBatch(syncBatch, destDB, true); err != nil {
			return chunkResult{}, err
		}
	}

	if sourceExhausted && readerBytes < ChunkBytes {
		return chunkResult{keys: writtenKeys, bytes: writtenBytes, lastKey: nil}, nil
	}
	return chunkResult{keys: writtenKeys, bytes: writtenBytes, lastKey: readerLastKey}, nil
}
