package network

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/stretchr/testify/require"
	"github.com/tendermint/tendermint/p2p/pex"
	"github.com/testground/sdk-go/run"
	"github.com/testground/sdk-go/runtime"
)

func TestOperator_StopJob(t *testing.T) {
	op := NewOperator()

	// Add a fake job.
	op.jobs["fake_job"] = func() {
		t.Log("Fake job stopped")
	}

	op.StopJob("fake_job")

	if _, exists := op.jobs["fake_job"]; exists {
		t.Errorf("Job should be removed after StopJob")
	}
}

func TestOperator_Run(t *testing.T) {
	// can't run this test if the runenv is actually used during the Run call
	t.Skip("Skipping TestOperator_Run")
	op := NewOperator()

	testDelay := time.Millisecond * 200

	// Register a command handler.
	op.registry["test_cmd"] = func(ctx context.Context, runenv *runtime.RunEnv, initCtx *run.InitContext, args json.RawMessage) error {
		time.Sleep(testDelay)
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runenv := &runtime.RunEnv{}
	initCtx := &run.InitContext{}

	cmds := make(chan Command)

	go func() {
		cmds <- Command{
			ID:      "test1",
			Name:    "test_cmd",
			Args:    nil,
			Timeout: time.Second,
		}
		close(cmds)
	}()

	start := time.Now()
	if err := op.Run(ctx, runenv, initCtx, cmds); err != nil {
		t.Errorf("Run returned error: %s", err)
	}
	end := time.Now()
	require.True(t, end.Sub(start) >= testDelay, "Run should block until all jobs are finished")

	require.Equal(t, 0, len(op.jobs), "All jobs should be canceled")
}

func TestOperator_Stop(t *testing.T) {
	op := NewOperator()
	_, cancel := context.WithCancel(context.Background())

	op.jobs["test_job"] = cancel

	op.Stop()

	if len(op.jobs) != 0 {
		t.Errorf("All jobs should be canceled")
	}
}

func TestAddrBookLoading(t *testing.T) {
	peerPacket := PeerPacket{
		GroupID:        "seeds",
		GlobalSequence: 4,
		PeerID:         "ad54e978933b00105c3615e65e6a27c5d27bdb29@192.168.0.15:26656",
	}
	temp := t.TempDir()

	tmcfg := app.DefaultConsensusConfig()
	tmcfg = tmcfg.SetRoot(temp)

	err := addPeersToAddressBook(tmcfg.P2P.AddrBookFile(), []PeerPacket{peerPacket})
	require.NoError(t, err)

	addrBook := pex.NewAddrBook(tmcfg.P2P.AddrBookFile(), false)
	err = addrBook.OnStart()
	require.NoError(t, err)

	require.False(t, addrBook.Empty())
	require.Equal(t, addrBook.Size(), 1)
}
