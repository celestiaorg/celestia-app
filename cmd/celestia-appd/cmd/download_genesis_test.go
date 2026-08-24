package cmd

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/celestiaorg/celestia-app/v10/pkg/appconsts"
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

	if err := downloadFile(destination, server.URL, "wrong hash"); err == nil {
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
	if err := downloadFile(destination, server.URL, hex.EncodeToString(hash[:])); err != nil {
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
