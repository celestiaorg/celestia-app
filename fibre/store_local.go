package fibre

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	"github.com/cockroachdb/pebble/v2/vfs"
)

const (
	shardsSubdir  = "shards"
	stagingSubdir = "staging"
)

// localBackend stores shard payloads as flat files.
type localBackend struct {
	path    string
	fs      vfs.FS
	metrics *serverMetrics
}

// shardPayloadWriteCategory labels shard payload bytes in VFS disk-write metrics.
const shardPayloadWriteCategory vfs.DiskWriteCategory = "fibre-shard"

func (*localBackend) backendTag() shardBackendTag {
	return localBackendTag
}

func newLocalBackend(path string, filesystem vfs.FS) (*localBackend, error) {
	for _, sub := range []string{shardsSubdir, stagingSubdir} {
		if err := filesystem.MkdirAll(filepath.Join(path, sub), 0o755); err != nil {
			return nil, fmt.Errorf("creating %s directory: %w", sub, err)
		}
	}
	return &localBackend{path: path, fs: filesystem}, nil
}

func (b *localBackend) Put(ctx context.Context, commitment Commitment, promiseHash []byte, shard *types.BlobShard) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}

	tmp, err := b.writeTmp(shard)
	if err != nil {
		return false, fmt.Errorf("writing shard tmp: %w", err)
	}
	defer func() { _ = b.fs.Remove(tmp) }()

	if err := ctx.Err(); err != nil {
		return false, fmt.Errorf("aborting shard publish: %w", err)
	}
	if err := b.fs.Rename(tmp, b.shardPath(commitment, promiseHash)); err != nil {
		return false, fmt.Errorf("renaming shard tmp to final: %w", err)
	}
	return true, nil
}

func (b *localBackend) Get(ctx context.Context, commitment Commitment, promiseHash []byte) (_ *types.BlobShard, err error) {
	done := b.metrics.observeBackendGet(ctx, storageBackendLocal)
	defer func() { done(err) }()
	f, err := b.fs.Open(b.shardPath(commitment, promiseHash))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrStoreNotFound
		}
		return nil, err
	}
	defer f.Close()
	// Buffer small codec reads to avoid per-field file reads and metric updates.
	return readShardBinary(bufio.NewReaderSize(b.metrics.backendReader(ctx, storageBackendLocal, f), 1<<20))
}

func (b *localBackend) Has(_ context.Context, commitment Commitment, promiseHash []byte) (bool, error) {
	_, err := b.fs.Stat(b.shardPath(commitment, promiseHash))
	switch {
	case errors.Is(err, os.ErrNotExist):
		return false, nil
	case err != nil:
		return false, fmt.Errorf("stat shard file: %w", err)
	default:
		return true, nil
	}
}

func (b *localBackend) Delete(ctx context.Context, commitment Commitment, promiseHash []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := b.fs.Remove(b.shardPath(commitment, promiseHash)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("removing shard file: %w", err)
	}
	return nil
}

func (b *localBackend) size(commitment Commitment, promiseHash []byte) (int64, error) {
	info, err := b.fs.Stat(b.shardPath(commitment, promiseHash))
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}

func (b *localBackend) diskAvailable() (int64, error) {
	du, err := b.fs.GetDiskUsage(b.path)
	if err != nil {
		return 0, fmt.Errorf("getting disk usage: %w", err)
	}
	return int64(du.AvailBytes), nil
}

// writeTmp uses a random per-writer name because vfs.FS.Create truncates
// colliding files. The final rename selects the same-key winner.
func (b *localBackend) writeTmp(shard *types.BlobShard) (string, error) {
	var rnd [16]byte
	if _, err := rand.Read(rnd[:]); err != nil {
		return "", fmt.Errorf("generating tmp name: %w", err)
	}
	tmp := filepath.Join(b.path, stagingSubdir, hex.EncodeToString(rnd[:]))

	f, err := b.fs.Create(tmp, shardPayloadWriteCategory)
	if err != nil {
		return "", fmt.Errorf("creating tmp shard file: %w", err)
	}
	bw := bufio.NewWriterSize(f, 1<<20)
	if err := writeShardBinary(bw, shard); err != nil {
		f.Close()
		_ = b.fs.Remove(tmp)
		return "", err
	}
	if err := bw.Flush(); err != nil {
		f.Close()
		_ = b.fs.Remove(tmp)
		return "", err
	}
	if err := f.Close(); err != nil {
		_ = b.fs.Remove(tmp)
		return "", err
	}
	return tmp, nil
}

func (b *localBackend) shardPath(commitment Commitment, promiseHash []byte) string {
	return filepath.Join(b.path, shardsSubdir, commitment.String()+"-"+hex.EncodeToString(promiseHash))
}

func (b *localBackend) resetStaging() (int, error) {
	stagingDir := filepath.Join(b.path, stagingSubdir)
	entries, err := b.fs.List(stagingDir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return 0, fmt.Errorf("reading staging: %w", err)
	}
	if err := b.fs.RemoveAll(stagingDir); err != nil {
		return len(entries), fmt.Errorf("removing staging: %w", err)
	}
	if err := b.fs.MkdirAll(stagingDir, 0o755); err != nil {
		return len(entries), fmt.Errorf("recreating staging: %w", err)
	}
	return len(entries), nil
}
