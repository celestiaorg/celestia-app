package user

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v10/app/encoding"
	"github.com/cosmos/cosmos-sdk/crypto/hd"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	sdktypes "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

func TestParallelQueueSize(t *testing.T) {
	encCfg := encoding.MakeConfig()
	keys := keyring.NewInMemory(encCfg.Codec)
	for i, name := range []string{"master", "parallel-worker-1"} {
		path := hd.CreateHDPath(sdktypes.CoinType, 0, uint32(i)).String()
		_, _, err := keys.NewMnemonic(name, keyring.English, path, keyring.DefaultBIP39Passphrase, hd.Secp256k1)
		require.NoError(t, err)
	}
	signer, err := NewSigner(keys, encCfg.TxConfig, "test-chain",
		NewAccount("master", 1, 0), NewAccount("parallel-worker-1", 2, 0))
	require.NoError(t, err)

	tests := []struct {
		name    string
		options []Option
		size    int
		workers int
	}{
		{"default", nil, defaultParallelQueueSize, 1},
		{"default with workers", []Option{WithTxWorkers(2)}, defaultParallelQueueSize, 2},
		{"size only", []Option{WithParallelQueueSize(1)}, 1, 1},
		{"size before workers", []Option{WithParallelQueueSize(1), WithTxWorkers(2)}, 1, 2},
		{"size after workers", []Option{WithTxWorkers(2), WithParallelQueueSize(1)}, 1, 2},
		{"workers replaced", []Option{WithTxWorkers(1), WithParallelQueueSize(1), WithTxWorkers(2)}, 1, 2},
		{"last size wins", []Option{WithParallelQueueSize(1), WithParallelQueueSize(2)}, 2, 1},
		{"large buffer", []Option{WithParallelQueueSize(10000)}, 10000, 1},
		{"unbuffered", []Option{WithParallelQueueSize(0)}, 0, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := NewTxClient(encCfg.Codec, signer, nil, encCfg.InterfaceRegistry, tt.options...)
			require.NoError(t, err)
			queue := client.txQueue
			t.Cleanup(queue.stop)
			require.Len(t, queue.workers, tt.workers)

			for _, stage := range []string{"start", "restart"} {
				require.NoError(t, queue.start(t.Context()))
				require.Equal(t, tt.size, cap(queue.jobQueue), stage)
				for _, worker := range queue.workers {
					require.Equal(t, queue.jobQueue, worker.jobQueue, stage)
				}
				queue.stop()
			}
		})
	}
}
