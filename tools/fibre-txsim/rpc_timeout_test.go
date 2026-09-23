package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRejectInvalidRPCTimeout(t *testing.T) {
	for _, timeout := range []time.Duration{0, -time.Second} {
		require.ErrorContains(t, run(config{concurrency: 1, blobSize: 1, rpcTimeout: timeout}), "--rpc-timeout must be > 0")
	}
}
