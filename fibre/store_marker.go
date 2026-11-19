package fibre

import (
	"encoding/binary"
	"fmt"

	"github.com/celestiaorg/celestia-app/v11/pkg/rsema1d/field"
	"github.com/celestiaorg/celestia-app/v11/pkg/rsema1d/merkle"
)

const (
	shardMarkerVersion = 1
	shardMarkerSize    = 10
	// packIDSize is the length of the random identifier of a packed object.
	packIDSize = 16
	// packedMarkerSize extends the base marker with the shard's byte offset
	// inside its packed object (8 bytes) and that object's pack identity.
	packedMarkerSize = shardMarkerSize + 8 + packIDSize
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
	// packedObjectBackendTag marks a shard whose payload is a byte range inside
	// a packed S3 object shared with other shards. Written only when
	// object_storage.shards_per_object > 1; older binaries cannot read it.
	packedObjectBackendTag shardBackendTag = 0x06

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
	if len(data) < 2 {
		return 0, 0, fmt.Errorf("%w: shard marker length %d is too short", ErrStoreIntegrity, len(data))
	}
	if data[0] != shardMarkerVersion {
		return 0, 0, fmt.Errorf("%w: unsupported shard marker version %d", ErrStoreIntegrity, data[0])
	}
	backend := shardBackendTag(data[1])
	wantLen := shardMarkerSize
	switch backend {
	case localBackendTag, objectBackendTag:
	case packedObjectBackendTag:
		wantLen = packedMarkerSize
	default:
		return 0, 0, fmt.Errorf("%w: unsupported shard backend tag 0x%02x", ErrStoreIntegrity, data[1])
	}
	if len(data) != wantLen {
		return 0, 0, fmt.Errorf("%w: shard marker length %d, want %d", ErrStoreIntegrity, len(data), wantLen)
	}
	size := binary.BigEndian.Uint64(data[2:shardMarkerSize])
	if size == 0 || size > uint64(maxPlausibleShardSize) {
		return 0, 0, fmt.Errorf("%w: invalid shard size %d", ErrStoreIntegrity, size)
	}
	return backend, int64(size), nil
}

// encodePackedShardMarker encodes a packed shard's size, its byte offset inside
// the packed object, and that object's pack identity.
func encodePackedShardMarker(size, offset int64, packID [packIDSize]byte) []byte {
	marker := make([]byte, packedMarkerSize)
	marker[0] = shardMarkerVersion
	marker[1] = byte(packedObjectBackendTag)
	binary.BigEndian.PutUint64(marker[2:shardMarkerSize], uint64(size))
	binary.BigEndian.PutUint64(marker[shardMarkerSize:shardMarkerSize+8], uint64(offset))
	copy(marker[shardMarkerSize+8:], packID[:])
	return marker
}

// decodePackedShardMarker returns the pack identity, byte offset, and size of a
// packed shard marker.
func decodePackedShardMarker(data []byte) (packID [packIDSize]byte, offset, size int64, err error) {
	tag, markerSize, err := decodeShardMarkerBackend(data)
	if err != nil {
		return packID, 0, 0, err
	}
	if tag != packedObjectBackendTag {
		return packID, 0, 0, fmt.Errorf("%w: shard marker is not packed", ErrStoreIntegrity)
	}
	offset = int64(binary.BigEndian.Uint64(data[shardMarkerSize : shardMarkerSize+8]))
	if offset < 0 || offset > maxPackedObjectBytes-markerSize {
		return packID, 0, 0, fmt.Errorf("%w: invalid packed offset %d", ErrStoreIntegrity, offset)
	}
	copy(packID[:], data[shardMarkerSize+8:])
	return packID, offset, markerSize, nil
}
