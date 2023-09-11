package sanity

import (
	"context"

	"github.com/celestiaorg/celestia-app/test/testground/network"
	"github.com/testground/sdk-go/run"
	"github.com/testground/sdk-go/runtime"
)

// SanityEntryPoint is a test case that does nothing but signal that the test plan has
// completed successfully. It is used to quickly explore and debug testground.
func SanityEntryPoint(runenv *runtime.RunEnv, initCtx *run.InitContext) (err error) {
	if err != nil {
		runenv.RecordFailure(err)
		initCtx.SyncClient.MustSignalAndWait(context.Background(), network.FinishedState, runenv.TestInstanceCount)
		return err
	}
	runenv.RecordSuccess()
	return err
}
