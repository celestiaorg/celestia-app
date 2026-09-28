package fibre

import (
	"encoding/binary"
	"fmt"
	"math"
)

const (
	shardMarkerVersion = 1
	shardMarkerSize    = 10
)

type shardBackendTag byte

const (
	localBackendTag  shardBackendTag = 0x01
	objectBackendTag shardBackendTag = 0x02

	storageBackendLocal  = "local"
	storageBackendObject = "object"
)

// encodeShardMarkerForBackend encodes a backend and shard size in a versioned marker.
func encodeShardMarkerForBackend(backend shardBackendTag, size int64) []byte {
	marker := make([]byte, shardMarkerSize)
	marker[0] = shardMarkerVersion
	marker[1] = byte(backend)
	binary.BigEndian.PutUint64(marker[2:], uint64(size))
	return marker
}

// decodeShardMarkerBackend returns the backend and size from a shard marker.
func decodeShardMarkerBackend(data []byte) (shardBackendTag, int64, error) {
	if len(data) == 0 {
		// Remove this legacy path after all stored shards use versioned markers.
		return localBackendTag, 0, nil
	}
	if len(data) != shardMarkerSize {
		return 0, 0, fmt.Errorf("%w: shard marker length %d, want %d", ErrStoreIntegrity, len(data), shardMarkerSize)
	}
	if data[0] != shardMarkerVersion {
		return 0, 0, fmt.Errorf("%w: unsupported shard marker version %d", ErrStoreIntegrity, data[0])
	}
	backend := shardBackendTag(data[1])
	if backend != localBackendTag && backend != objectBackendTag {
		return 0, 0, fmt.Errorf("%w: unsupported shard backend tag 0x%02x", ErrStoreIntegrity, data[1])
	}
	size := binary.BigEndian.Uint64(data[2:])
	if size == 0 || size > math.MaxInt64 {
		return 0, 0, fmt.Errorf("%w: invalid shard size %d", ErrStoreIntegrity, size)
	}
	return backend, int64(size), nil
}
