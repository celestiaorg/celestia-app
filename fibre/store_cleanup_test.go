package fibre

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
	"github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	pebbledb "github.com/cockroachdb/pebble/v2"
	"github.com/cockroachdb/pebble/v2/vfs/errorfs"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	"github.com/stretchr/testify/require"
)

func TestStorePutCommitFailureCleanup(t *testing.T) {
	for _, markerState := range []string{"missing", "present", "read error"} {
		t.Run(markerState, func(t *testing.T) {
			store := newMarkerTestStore(t)
			filesystem := storeLocalBackend(t, store).fs
			promise := &PaymentPromise{
				ChainID: "test-chain", SignerKey: secp256k1.GenPrivKey().PubKey().(*secp256k1.PubKey),
				Commitment: generateCommitment(), CreationTimestamp: time.Unix(1, 0), Signature: []byte{1},
			}
			hash, err := promise.Hash()
			require.NoError(t, err)
			if markerState != "missing" {
				require.NoError(t, store.db.Set(shardKey(promise.Commitment, hash), encodeShardMarkerForBackend(objectBackendTag, 1), pebbledb.NoSync))
				require.NoError(t, store.db.Flush())
			}
			require.NoError(t, store.db.Close())
			var failReads atomic.Bool
			filesystem = errorfs.Wrap(filesystem, errorfs.InjectorFunc(func(op errorfs.Op) error {
				if failReads.Load() && op.Kind == errorfs.OpFileReadAt {
					return errorfs.ErrInjected
				}
				return nil
			}))
			// A read-only database makes the metadata commit fail after the object PUT.
			store.db, err = pebbledb.Open(memStorePath, &pebbledb.Options{FS: filesystem, ReadOnly: true})
			require.NoError(t, err)
			failReads.Store(markerState == "read error")
			defer failReads.Store(false)
			deletes := 0
			client := &s3ObjectClientStub{
				putObject: func(context.Context, *s3.PutObjectInput, ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
					// A retry can see the object created by an earlier attempt.
					return nil, &smithy.GenericAPIError{Code: "PreconditionFailed"}
				},
				deleteObject: func(context.Context, *s3.DeleteObjectInput, ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
					deletes++
					return &s3.DeleteObjectOutput{}, nil
				},
			}
			store.shards = newRoutedStorage(newObjectBackend(client, objectNamespace{Bucket: "bucket"}), nil)
			err = store.Put(t.Context(), promise, &types.BlobShard{}, promise.CreationTimestamp)
			require.ErrorIs(t, err, pebbledb.ErrReadOnly)
			require.ErrorContains(t, err, "committing metadata")
			if markerState == "missing" {
				require.Equal(t, 1, deletes)
				var active atomic.Bool
				client.putObject = func(context.Context, *s3.PutObjectInput, ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
					if active.Swap(true) {
						t.Error("another same-key write started before cleanup finished")
					}
					return nil, &smithy.GenericAPIError{Code: "PreconditionFailed"}
				}
				client.deleteObject = func(context.Context, *s3.DeleteObjectInput, ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
					// Keep cleanup in progress while the other writers try to enter Put.
					time.Sleep(time.Millisecond)
					active.Store(false)
					return &s3.DeleteObjectOutput{}, nil
				}
				var writers sync.WaitGroup
				for range 10 {
					writers.Go(func() {
						err := store.Put(t.Context(), promise, &types.BlobShard{}, promise.CreationTimestamp)
						if !errors.Is(err, pebbledb.ErrReadOnly) {
							t.Errorf("expected failed metadata commit, got %v", err)
						}
					})
				}
				writers.Wait()
			} else {
				require.Zero(t, deletes)
			}
			if markerState == "read error" {
				_, _, err := store.db.Get(shardKey(promise.Commitment, hash))
				require.ErrorIs(t, err, errorfs.ErrInjected)
			}
		})
	}
}
