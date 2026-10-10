package cmd

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v10/pkg/appconsts"
	"github.com/cosmos/cosmos-sdk/server"
)

func TestIsKnownChainID(t *testing.T) {
	tests := []struct {
		chainID string
		want    bool
	}{
		{appconsts.MainnetChainID, true},
		{appconsts.MochaChainID, true},
		{appconsts.CortoChainID, true},
		{"foo", false},
	}

	for _, tt := range tests {
		t.Run(tt.chainID, func(t *testing.T) {
			if got := isKnownChainID(tt.chainID); got != tt.want {
				t.Fatalf("isKnownChainID(%q) = %v, want %v", tt.chainID, got, tt.want)
			}
		})
	}
}

func TestDownloadFilePreservesDestinationOnHashMismatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("downloaded"))
	}))
	defer server.Close()

	destination := filepath.Join(t.TempDir(), "genesis.json")
	if err := os.WriteFile(destination, []byte("existing"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := downloadFile(context.Background(), destination, server.URL, "wrong hash"); err == nil {
		t.Fatal("downloadFile returned nil, want hash mismatch")
	}
	got, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "existing" {
		t.Fatalf("destination contains %q, want existing content", got)
	}
}

func TestDownloadFileReplacesDestinationAfterVerification(t *testing.T) {
	content := []byte("downloaded")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(content)
	}))
	defer server.Close()

	destination := filepath.Join(t.TempDir(), "genesis.json")
	hash := sha256.Sum256(content)
	if err := downloadFile(context.Background(), destination, server.URL, hex.EncodeToString(hash[:])); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(content) {
		t.Fatalf("destination contains %q, want %q", got, content)
	}
}

func TestDownloadFileCancelsRequestWithContext(t *testing.T) {
	started := make(chan struct{})
	released := make(chan struct{})
	var releaseOnce sync.Once
	releaseHandler := func() {
		releaseOnce.Do(func() { close(released) })
	}
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		close(started)
		select {
		case <-r.Context().Done():
		case <-released:
		}
	}))
	defer server.Close()
	defer releaseHandler()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	destination := filepath.Join(t.TempDir(), "genesis.json")
	done := make(chan error, 1)
	go func() {
		done <- downloadFile(ctx, destination, server.URL, "unused")
	}()

	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("HTTP handler did not start")
	}
	cancel()
	select {
	case err := <-done:
		if err == nil || !errors.Is(err, context.Canceled) {
			t.Fatalf("downloadFile error = %v, want context cancellation", err)
		}
	case <-time.After(5 * time.Second):
		releaseHandler()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("downloadFile did not return after releasing HTTP handler")
		}
		t.Fatal("downloadFile did not return after context cancellation")
	}
}

func TestDownloadGenesisCommandAppliesTimeout(t *testing.T) {
	const timeout = 50 * time.Millisecond
	cmd := newDownloadGenesisCommand(timeout, func(ctx context.Context, _, _, _ string) error {
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > timeout {
			return errors.New("download context is missing the command timeout")
		}
		<-ctx.Done()
		return ctx.Err()
	})
	sctx := server.NewDefaultContext()
	cmd.SetContext(context.WithValue(context.Background(), server.ServerContextKey, sctx))

	err := cmd.RunE(cmd, []string{appconsts.MainnetChainID})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("command error = %v, want deadline exceeded", err)
	}
}
