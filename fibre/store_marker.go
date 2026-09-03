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

const localBackendTag shardBackendTag = 0x01

func encodeShardMarker(size int64) ([]byte, error) {
	if size <= 0 {
		return nil, fmt.Errorf("invalid shard size %d", size)
	}

	marker := make([]byte, shardMarkerSize)
	marker[0] = shardMarkerVersion
	marker[1] = byte(localBackendTag)
	binary.BigEndian.PutUint64(marker[2:], uint64(size))
	return marker, nil
}

func decodeShardMarker(data []byte) (int64, error) {
	if len(data) == 0 {
		return 0, nil
	}
	if len(data) != shardMarkerSize {
		return 0, fmt.Errorf("%w: shard marker length %d, want %d", ErrStoreIntegrity, len(data), shardMarkerSize)
	}
	if data[0] != shardMarkerVersion {
		return 0, fmt.Errorf("%w: unsupported shard marker version %d", ErrStoreIntegrity, data[0])
	}
	if shardBackendTag(data[1]) != localBackendTag {
		return 0, fmt.Errorf("%w: unsupported shard backend tag 0x%02x", ErrStoreIntegrity, data[1])
	}

	size := binary.BigEndian.Uint64(data[2:])
	if size == 0 || size > math.MaxInt64 {
		return 0, fmt.Errorf("%w: invalid shard size %d", ErrStoreIntegrity, size)
	}
	return int64(size), nil
}
