package abci

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	clienthelpers "cosmossdk.io/client/v2/helpers"
	"cosmossdk.io/log"
	"github.com/celestiaorg/celestia-app/v10/multiplexer/appd"
	db "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/server"
	serverconfig "github.com/cosmos/cosmos-sdk/server/config"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"
)

// TestGetAppSwitchesVersionInPlaceWhenBinaryIsShared verifies that when two
// registered app versions are served by the same embedded binary (as app
// versions 1, 2 and 3 are all served by celestia-app v3), moving from one to
// the other updates the active version without stopping and restarting the
// process.
func TestGetAppSwitchesVersionInPlaceWhenBinaryIsShared(t *testing.T) {
	// The mock binary appends "started" when it comes up and "interrupted"
	// when it receives the interrupt that Appd.Stop sends. Stop waits for the
	// child to exit, so if getApp restarted the binary the log would contain
	// "interrupted" by the time getApp returns.
	logPath := filepath.Join(t.TempDir(), "lifecycle.log")
	shared := newMockAppd(t, "v0.0.0-shared-binary-test", mockAppdScript(logPath))

	versions, err := NewVersions(
		Version{AppVersion: 1, ABCIVersion: ABCIClientVersion1, Appd: shared},
		Version{AppVersion: 2, ABCIVersion: ABCIClientVersion1, Appd: shared},
	)
	require.NoError(t, err)

	m := newTestMultiplexer(t, versions, 1)

	_, err = m.getApp()
	require.NoError(t, err)
	require.Equal(t, uint64(1), m.activeVersion.AppVersion)
	require.Eventually(t, func() bool { return readFile(t, logPath) == "started\n" }, 5*time.Second, 10*time.Millisecond)

	m.appVersion = 2
	_, err = m.getApp()
	require.NoError(t, err)
	require.Equal(t, uint64(2), m.activeVersion.AppVersion)
	require.True(t, shared.IsRunning(), "the shared binary must keep running across the switch")
	require.Equal(t, "started\n", readFile(t, logPath), "the shared binary must not be restarted")
}

// TestGetAppRestartsWhenBinaryDiffers verifies that moving to an app version
// served by a different embedded binary stops the old process and starts the
// new one.
func TestGetAppRestartsWhenBinaryDiffers(t *testing.T) {
	oldLogPath := filepath.Join(t.TempDir(), "old.log")
	oldAppd := newMockAppd(t, "v0.0.0-old-binary-test", mockAppdScript(oldLogPath))
	newAppd := newMockAppd(t, "v0.0.0-new-binary-test", mockAppdScript(filepath.Join(t.TempDir(), "new.log")))

	versions, err := NewVersions(
		Version{AppVersion: 1, ABCIVersion: ABCIClientVersion1, Appd: oldAppd},
		Version{AppVersion: 2, ABCIVersion: ABCIClientVersion2, Appd: newAppd},
	)
	require.NoError(t, err)

	m := newTestMultiplexer(t, versions, 1)

	_, err = m.getApp()
	require.NoError(t, err)
	require.True(t, oldAppd.IsRunning())
	require.True(t, newAppd.IsStopped())
	require.Eventually(t, func() bool { return readFile(t, oldLogPath) == "started\n" }, 5*time.Second, 10*time.Millisecond)

	m.appVersion = 2
	_, err = m.getApp()
	require.NoError(t, err)
	require.Equal(t, uint64(2), m.activeVersion.AppVersion)
	require.True(t, oldAppd.IsStopped(), "the old binary must be stopped")
	require.True(t, newAppd.IsRunning(), "the new binary must be started")
}

// mockAppdScript returns a shell script that appends "started" to logPath
// once it is up and "interrupted" when it receives the interrupt sent by
// Appd.Stop, then exits cleanly. It stays alive on a background sleep that is
// killed on exit so no orphan outlives the test.
func mockAppdScript(logPath string) string {
	return strings.Join([]string{
		"sleep 30 &",
		"trap 'kill $! 2>/dev/null; echo interrupted >> " + logPath + "; exit 0' INT TERM",
		"echo started >> " + logPath,
		"wait",
	}, "\n")
}

// TestGetAppFailsForUnsupportedAppVersion verifies that an app version older
// than every registered embedded version is a hard error rather than a silent
// fallback to the native app.
func TestGetAppFailsForUnsupportedAppVersion(t *testing.T) {
	versions, err := NewVersions(
		Version{AppVersion: 2, ABCIVersion: ABCIClientVersion1, Appd: &appd.Appd{}},
		Version{AppVersion: 3, ABCIVersion: ABCIClientVersion1, Appd: &appd.Appd{}},
	)
	require.NoError(t, err)

	serverContext := server.NewDefaultContext()
	serverContext.Config.SetRoot(t.TempDir())
	nilAppCreator := func(log.Logger, db.DB, io.Writer, servertypes.AppOptions) servertypes.Application { return nil }
	m, err := NewMultiplexer(serverContext, serverconfig.Config{}, client.Context{}, nilAppCreator, versions, "test-chain", 1)
	require.NoError(t, err)

	_, err = m.getApp()
	require.ErrorIs(t, err, ErrUnsupportedAppVersion)
	require.False(t, m.started, "no app must be started for an unsupported app version")
}

// newTestMultiplexer returns a Multiplexer wired with just enough state for
// getApp to start and switch embedded apps. Any running embedded app is
// stopped when the test ends.
func newTestMultiplexer(t *testing.T, versions Versions, appVersion uint64) *Multiplexer {
	t.Helper()
	m := &Multiplexer{
		logger:     log.NewNopLogger(),
		versions:   versions,
		appVersion: appVersion,
	}
	m.g, m.ctx = errgroup.WithContext(t.Context())
	t.Cleanup(func() {
		require.NoError(t, m.stopEmbeddedApp())
	})
	return m
}

// newMockAppd builds an Appd whose binary is a shell script running command.
// It goes through appd.New so the Appd behaves exactly like an embedded
// release binary, which means it is extracted under the node home like the
// real ones; the extracted directory is removed when the test ends.
func newMockAppd(t *testing.T, version, command string) *appd.Appd {
	t.Helper()

	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	script := []byte("#!/bin/sh\n" + command + "\n")
	require.NoError(t, tw.WriteHeader(&tar.Header{Name: "celestia-appd", Mode: 0o755, Size: int64(len(script))}))
	_, err := tw.Write(script)
	require.NoError(t, err)
	require.NoError(t, tw.Close())
	require.NoError(t, gw.Close())

	// Mirror appd's own home resolution so a stale directory from an earlier
	// run never masks the freshly built script.
	nodeHome, err := clienthelpers.GetNodeHomeDirectory(".celestia-app")
	require.NoError(t, err)
	versionDir := filepath.Join(nodeHome, "bin", version)
	require.NoError(t, os.RemoveAll(versionDir))
	t.Cleanup(func() { _ = os.RemoveAll(versionDir) })

	a, err := appd.New(version, buf.Bytes())
	require.NoError(t, err)
	return a
}

// readFile returns the contents of path, or "" if it does not exist yet.
func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return ""
	}
	require.NoError(t, err)
	return string(data)
}
