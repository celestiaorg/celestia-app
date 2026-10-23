package fibre

import (
	"encoding/binary"
	"fmt"

	"github.com/celestiaorg/celestia-app/v10/pkg/rsema1d/field"
	"github.com/celestiaorg/celestia-app/v10/pkg/rsema1d/merkle"
)

const (
	shardMarkerVersion = 1
	shardMarkerSize    = 10
)

// maxPlausibleShardSize is the serialized size of the largest shard a
// validator can accept under [DefaultProtocolParams].
var maxPlausibleShardSize = maxShardBinarySize(DefaultProtocolParams)

// maxShardBinarySize returns the largest [shardBinarySize] of a shard that
// passes upload verification under p: one RLC per original row, and at most
// [ProtocolParams.MaxRowsPerValidator] rows of [ProtocolParams.MaxRowSize]
// bytes, each with a [ProtocolParams.MerkleProofDepth]-segment proof.
func maxShardBinarySize(p ProtocolParams) int64 {
	// version + rlcs-len prefix + rlcs + rows-count prefix
	size := int64(4 + 4 + p.Rows*field.GF128Size + 4)
	// index + data-len prefix + data + proof-count prefix + (seg-len prefix + seg) per segment
	rowSize := int64(4 + 4 + p.MaxRowSize(0) + 4 + p.MerkleProofDepth()*(4+merkle.NodeSize))
	return size + int64(p.MaxRowsPerValidator())*rowSize
}

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
	if size == 0 || size > uint64(maxPlausibleShardSize) {
		return 0, 0, fmt.Errorf("%w: invalid shard size %d", ErrStoreIntegrity, size)
	}
	return backend, int64(size), nil
}
