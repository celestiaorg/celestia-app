package fibre

import (
	"crypto/sha256"
	"encoding/binary"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUploadLockDistributionAndExclusion(t *testing.T) {
	var server Server
	seen := make(map[*sync.Mutex][]byte)
	for i := uint64(0); i < 65536; i++ {
		var input [8]byte
		binary.BigEndian.PutUint64(input[:], i)
		hash := sha256.Sum256(input[:])
		lock := server.uploadLock(hash[:])
		require.Same(t, lock, server.uploadLock(append([]byte(nil), hash[:]...)))
		if previous, ok := seen[lock]; ok {
			lock.Lock()
			require.False(t, server.uploadLock(previous).TryLock(), "colliding uploads must serialize")
			lock.Unlock()
		} else {
			seen[lock] = append([]byte(nil), hash[:]...)
		}
	}
	require.Len(t, seen, 2048, "cryptographic hashes must reach every upload stripe")
}
