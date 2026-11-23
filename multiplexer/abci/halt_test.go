package abci

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	abci "github.com/cometbft/cometbft/abci/types"
	serverconfig "github.com/cosmos/cosmos-sdk/server/config"
	"github.com/stretchr/testify/require"
)

// TestFinalizeBlockHaltStopsEmbeddedApp verifies that reaching halt-height
// stops the running embedded app. CometBFT panics on the returned error, so
// nothing else would stop the child and it would be left running.
func TestFinalizeBlockHaltStopsEmbeddedApp(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "halt.log")
	child := newMockAppd(t, "v0.0.0-halt-height-test", mockAppdScript(logPath))
	versions, err := NewVersions(Version{AppVersion: 1, ABCIVersion: ABCIClientVersion1, Appd: child})
	require.NoError(t, err)

	m := newTestMultiplexer(t, versions, 1)
	m.svrCfg = serverconfig.Config{BaseConfig: serverconfig.BaseConfig{HaltHeight: 5}}

	_, err = m.getApp()
	require.NoError(t, err)
	require.True(t, child.IsRunning())
	require.Eventually(t, func() bool { return readFile(t, logPath) == "started\n" }, 5*time.Second, 10*time.Millisecond)

	_, err = m.FinalizeBlock(context.Background(), &abci.RequestFinalizeBlock{Height: 5})
	require.ErrorContains(t, err, "halting node per configuration at height 5")
	require.True(t, child.IsStopped(), "the embedded app must be stopped when halting")
}
