package fibre

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestClientConfigSetMaxBlobSizeScalesRPCTimeout(t *testing.T) {
	cfg := DefaultClientConfig()
	require.NoError(t, cfg.SetMaxBlobSize(1280<<20))
	require.Equal(t, 150*time.Second, cfg.RPCTimeout)

	cfg = DefaultClientConfig()
	require.NoError(t, cfg.SetMaxBlobSize(DefaultProtocolParams.MaxBlobSize))
	require.Equal(t, DefaultClientConfig().RPCTimeout, cfg.RPCTimeout)

	cfg = DefaultClientConfig()
	require.NoError(t, cfg.SetMaxBlobSize(64<<20))
	require.Equal(t, DefaultClientConfig().RPCTimeout, cfg.RPCTimeout, "smaller blobs keep the default")
}
