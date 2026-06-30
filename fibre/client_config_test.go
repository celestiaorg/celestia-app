package fibre_test

import (
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v10/fibre"
	"github.com/stretchr/testify/assert"
)

// TestClientConfigTimeouts checks the split timeouts: RPCTimeout stays at the
// short 15s used for download/dial/host-query shedding, while the upload-only
// UploadTimeout accommodates the server's worst-case rate-limit wait
// (MaxBlobSize/rate == 12.8s) plus processing margin, staying well below the
// ~75s kernel TCP SYN window that black-hole peer shedding relies on.
func TestClientConfigTimeouts(t *testing.T) {
	cfg := fibre.DefaultClientConfig()

	p := fibre.DefaultProtocolParams
	burst := p.MaxRowSize(0) * p.Rows
	rateLimitWait := time.Duration(burst) * time.Second / time.Duration(fibre.DefaultUploadRateLimitBytesPerSecond)

	assert.Equal(t, 128*1024*1024, burst, "default burst should be MaxBlobSize")
	assert.Equal(t, 12800*time.Millisecond, rateLimitWait, "worst-case rate-limit wait should be 12.8s")

	// RPCTimeout (download/dial/host-query) is unchanged at 15s.
	assert.Equal(t, 15*time.Second, cfg.RPCTimeout)

	// UploadTimeout must exceed the worst-case server rate-limit wait so a client
	// can outlast a throttling validator and still reach quorum...
	assert.Greater(t, cfg.UploadTimeout, rateLimitWait)
	// ...and remain comfortably under the ~75s black-hole-shedding window.
	assert.Less(t, cfg.UploadTimeout, 75*time.Second)
}
