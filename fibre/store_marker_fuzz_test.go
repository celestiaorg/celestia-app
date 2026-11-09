package fibre

import (
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"
)

// FuzzShardMarkerDecode feeds arbitrary bytes to the stored shard marker
// decoder. Only the legacy empty marker and versioned markers naming a known
// backend with a size in (0, maxPlausibleShardSize] may decode, and every
// versioned marker that decodes must re-encode to the same bytes.
func FuzzShardMarkerDecode(f *testing.F) {
	f.Add([]byte{})
	f.Add(encodeShardMarkerForBackend(localBackendTag, 1))
	f.Add(encodeShardMarkerForBackend(objectBackendTag, maxPlausibleShardSize))
	f.Add(encodeShardMarkerForBackend(objectBackendTag, maxPlausibleShardSize+1))
	f.Add(encodeShardMarkerForBackend(localBackendTag, -1))
	f.Add([]byte{shardMarkerVersion, 0x03, 0, 0, 0, 0, 0, 0, 0, 1})

	f.Fuzz(func(t *testing.T, data []byte) {
		backend, size, err := decodeShardMarkerBackend(data)
		if len(data) == 0 {
			require.NoError(t, err)
			require.Equal(t, localBackendTag, backend)
			require.Zero(t, size)
			return
		}
		valid := len(data) == shardMarkerSize && data[0] == shardMarkerVersion &&
			(data[1] == byte(localBackendTag) || data[1] == byte(objectBackendTag))
		if valid {
			raw := binary.BigEndian.Uint64(data[2:])
			valid = raw != 0 && raw <= uint64(maxPlausibleShardSize)
		}
		if !valid {
			require.ErrorIs(t, err, ErrStoreIntegrity)
			return
		}
		require.NoError(t, err)
		require.Positive(t, size)
		require.LessOrEqual(t, size, maxPlausibleShardSize)
		require.Equal(t, data, encodeShardMarkerForBackend(backend, size), "marker does not round trip")
	})
}
