package abci

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	clienthelpers "cosmossdk.io/client/v2/helpers"
	"cosmossdk.io/log"
	"github.com/celestiaorg/celestia-app/v10/multiplexer/appd"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"
)

// mockAppdScript returns a shell script that appends "started" to logPath
// once it is up and "interrupted" when it receives the interrupt sent by
// Appd.Stop, then exits cleanly. It stays alive on a background sleep that is
// killed and reaped on exit so no orphan outlives the test.
func mockAppdScript(logPath string) string {
	return strings.Join([]string{
		"sleep 30 &",
		"trap 'kill $! 2>/dev/null; wait $! 2>/dev/null; echo interrupted >> " + logPath + "; exit 0' INT TERM",
		"echo started >> " + logPath,
		"wait",
	}, "\n")
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
