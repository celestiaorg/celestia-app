package types_test

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v11/x/valaddr/types"
	"github.com/stretchr/testify/require"
)

func TestValidateHost(t *testing.T) {
	valid := []string{
		"validator.example.com:7980",
		"h:1",
		"localhost:9090",
		"validator_1.internal:443",
		"127.0.0.1:1",
		"[::1]:65535",
		"[2001:db8::1]:443",
	}
	for _, host := range valid {
		require.NoError(t, types.ValidateHost(host), host)
	}

	invalid := []string{
		"",
		"host",
		"host:0",
		"host:65536",
		":80",
		// gRPC resolver schemes, which a client would dial instead of a host.
		"unix:9090",
		"UNIX:9090",
		"unix-abstract:9090",
		"passthrough:9090",
		"dns:9090",
		// Non-canonical spellings.
		"[host]:80",
		"[127.0.0.1]:80",
		"host:+80",
		"host:080",
		// Characters that are not part of a host name.
		"host/path:80",
		"user@host:80",
		"-host:80",
		"host.:80",
		"[fe80::1%eth0]:80",
	}
	for _, host := range invalid {
		require.ErrorIs(t, types.ValidateHost(host), types.ErrInvalidHostAddress, host)
	}
}
