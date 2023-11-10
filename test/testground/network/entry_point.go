package network

import (
	"github.com/celestiaorg/celestia-app/test/testground/compositions"
	"github.com/testground/sdk-go/run"
	"github.com/testground/sdk-go/runtime"
)

const FailedState = "failed"

// EntryPoint is the universal entry point for all role based tests.
func EntryPoint(runenv *runtime.RunEnv, initCtx *run.InitContext) error {
	initCtx, ctx, cancel, err := compositions.InitTest(runenv, initCtx)
	if err != nil {
		runenv.RecordFailure(err)
		initCtx.SyncClient.MustSignalAndWait(ctx, FailedState, runenv.TestInstanceCount)
		return err
	}
	defer cancel()

	// determine roles based only on the global sequence number. This allows for
	// us to deterministically calculate the IP addresses of each node.
	role, err := NewRole(runenv, initCtx)
	if err != nil {
		runenv.RecordFailure(err)
		initCtx.SyncClient.MustSignalAndWait(ctx, FailedState, runenv.TestInstanceCount)
		return err
	}

	// The plan step is responsible for creating and distributing all network
	// configurations including the genesis, keys, node types, topology, etc
	// using the parameters defined in the manifest and plan toml files. The
	// single "leader" role performs creation and publishing of the configs,
	// while the "follower" roles download the configs from the leader.
	err = role.Plan(ctx, runenv, initCtx)
	if err != nil {
		runenv.RecordFailure(err)
		initCtx.SyncClient.MustSignalAndWait(ctx, FailedState, runenv.TestInstanceCount)
		return err
	}

	// The execute step is responsible for starting the node and/or running any
	// tests.
	err = role.Execute(ctx, runenv, initCtx)
	if err != nil {
		runenv.RecordFailure(err)
		initCtx.SyncClient.MustSignalAndWait(ctx, FailedState, runenv.TestInstanceCount)
		return err
	}

	// The retro step is responsible for collecting any data from the node and/or
	// running any retrospective tests or benchmarks.
	err = role.Retro(ctx, runenv, initCtx)
	if err != nil {
		runenv.RecordFailure(err)
		initCtx.SyncClient.MustSignalAndWait(ctx, FailedState, runenv.TestInstanceCount)
		return err
	}

	// signal that the test has completed successfully
	runenv.RecordSuccess()
	return nil
}
