package network

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/testground/sdk-go/run"
	"github.com/testground/sdk-go/runtime"
)

func TestOperator_StopJob(t *testing.T) {
	op := NewOperator()

	// Add a fake job.
	op.jobs["fake_job"] = func() {
		t.Log("Fake job stoped")
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
