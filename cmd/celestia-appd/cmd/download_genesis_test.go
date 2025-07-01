package cmd

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsKnownChainID(t *testing.T) {
	tests := []struct {
		name     string
		chainID  string
		expected bool
	}{
		{
			name:     "known chain ID - celestia",
			chainID:  "celestia",
			expected: true,
		},
		{
			name:     "known chain ID - mocha-4",
			chainID:  "mocha-4",
			expected: true,
		},
		{
			name:     "unknown chain ID",
			chainID:  "unknown-chain",
			expected: false,
		},
		{
			name:     "empty chain ID",
			chainID:  "",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isKnownChainID(tt.chainID)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestChainIDs(t *testing.T) {
	result := chainIDs()

	// Check that all known chain IDs are included
	expectedChains := []string{"celestia", "mocha-4", "arabica-10", "arabica-11"}
	for _, chainID := range expectedChains {
		require.Contains(t, result, chainID)
	}

	// Check that result is comma-separated
	require.Contains(t, result, ",")
}

func TestGetChainIDOrDefault(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		expected string
	}{
		{
			name:     "no arguments",
			args:     []string{},
			expected: "celestia",
		},
		{
			name:     "one argument",
			args:     []string{"mocha-4"},
			expected: "mocha-4",
		},
		{
			name:     "multiple arguments",
			args:     []string{"arabica-10", "celestia"},
			expected: "celestia",
		},
		{
			name:     "empty string argument",
			args:     []string{""},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getChainIDOrDefault(tt.args)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestDownloadGenesis(t *testing.T) {
	tempDir := t.TempDir()

	// Content that matches the expected hash for celestia
	correctContent := `{"genesis_time":"2023-01-01T00:00:00Z","chain_id":"celestia"}`
	correctHasher := sha256.New()
	correctHasher.Write([]byte(correctContent))
	correctHash := hex.EncodeToString(correctHasher.Sum(nil))

	// Different content that will cause hash mismatch
	wrongContent := `{"genesis_time":"2023-01-01T00:00:00Z","chain_id":"wrong"}`

	// Create test server to mock genesis file responses
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/celestia/genesis.json"):
			// Return correct content for celestia
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(correctContent))
		case strings.Contains(r.URL.Path, "/mocha-4/genesis.json"):
			// Return wrong content for mocha-4 to trigger hash mismatch
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(wrongContent))
		case strings.Contains(r.URL.Path, "/arabica-10/genesis.json"):
			// Simulate server error
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("Internal Server Error"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	// Override the base URL to use our test server
	originalBaseURL := genesisBaseURL
	genesisBaseURL = server.URL
	defer func() {
		genesisBaseURL = originalBaseURL
	}()

	// Temporarily modify chainIDToSha256 for testing
	originalChainIDToSha256 := chainIDToSha256
	chainIDToSha256 = map[string]string{
		"celestia":   correctHash,                             // Correct hash - should pass
		"mocha-4":    "this_is_definitely_not_the_right_hash", // Wrong hash - should fail
		"arabica-10": correctHash,                             // Hash won't matter since server returns error
	}
	defer func() {
		chainIDToSha256 = originalChainIDToSha256
	}()

	tests := []struct {
		name        string
		chainID     string
		outputFile  string
		wantErr     bool
		errContains string
	}{
		{
			name:       "successful download with correct hash",
			chainID:    "celestia",
			outputFile: filepath.Join(tempDir, "celestia.json"),
			wantErr:    false,
		},
		{
			name:        "wrong hash - downloaded content doesn't match expected hash",
			chainID:     "mocha-4",
			outputFile:  filepath.Join(tempDir, "mocha-4.json"),
			wantErr:     true,
			errContains: "sha256 hash mismatch",
		},
		{
			name:        "server error - downloaded error content has wrong hash",
			chainID:     "arabica-10",
			outputFile:  filepath.Join(tempDir, "arabica-10.json"),
			wantErr:     true,
			errContains: "sha256 hash mismatch",
		},
		{
			name:        "unknown chain ID",
			chainID:     "unknown-chain",
			outputFile:  filepath.Join(tempDir, "unknown.json"),
			wantErr:     true,
			errContains: "unknown chain-id",
		},
		{
			name:        "invalid output file path",
			chainID:     "celestia",
			outputFile:  "/root/invalid/path/genesis.json",
			wantErr:     true,
			errContains: "error downloading / persisting the genesis file",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := downloadGenesis(tt.chainID, tt.outputFile)

			if tt.wantErr {
				require.Error(t, err)
				require.ErrorContains(t, err, tt.errContains)
			} else {
				require.NoError(t, err)
				// Verify file was created successfully
				if _, err := os.Stat(tt.outputFile); os.IsNotExist(err) {
					t.Errorf("DownloadGenesis() file was not created: %s", tt.outputFile)
				}
			}
		})
	}
}

func TestComputeSha256(t *testing.T) {
	tempDir := t.TempDir()

	tests := []struct {
		name    string
		content string
	}{
		{
			name:    "valid file",
			content: "test content",
		},
		{
			name:    "empty file",
			content: "",
		},
		{
			name:    "json content",
			content: `{"test": "value", "number": 123}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test file
			testFile := filepath.Join(tempDir, fmt.Sprintf("test_%s.json", tt.name))
			err := os.WriteFile(testFile, []byte(tt.content), 0o644)
			require.NoError(t, err)
			// Compute hash
			hash, err := computeSha256(testFile)

			require.NoError(t, err)

			// Verify hash is correct
			hasher := sha256.New()
			hasher.Write([]byte(tt.content))
			expectedHash := hex.EncodeToString(hasher.Sum(nil))

			require.Equal(t, expectedHash, hash)
		})
	}

	// Test with non-existent file
	t.Run("non-existent file", func(t *testing.T) {
		_, err := computeSha256("/non/existent/file.json")
		require.Error(t, err)
	})
}

func TestDownloadFile(t *testing.T) {
	tempDir := t.TempDir()

	// Create test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/success":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("success content"))
		case "/empty":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(""))
		case "/not-found":
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte("Not Found"))
		default:
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("Internal Server Error"))
		}
	}))
	defer server.Close()

	tests := []struct {
		name          string
		url           string
		filepath      string
		wantErr       bool
		errContains   string
		expectContent string
	}{
		{
			name:          "successful download",
			url:           server.URL + "/success",
			filepath:      filepath.Join(tempDir, "success.txt"),
			wantErr:       false,
			expectContent: "success content",
		},
		{
			name:          "empty response",
			url:           server.URL + "/empty",
			filepath:      filepath.Join(tempDir, "empty.txt"),
			wantErr:       false,
			expectContent: "",
		},
		{
			name:          "404 not found",
			url:           server.URL + "/not-found",
			filepath:      filepath.Join(tempDir, "not-found.txt"),
			wantErr:       false, // HTTP errors don't cause downloadFile to fail, they just download the error response
			expectContent: "Not Found",
		},
		{
			name:        "invalid URL",
			url:         "invalid-url",
			filepath:    filepath.Join(tempDir, "invalid.txt"),
			wantErr:     true,
			errContains: "",
		},
		{
			name:        "invalid file path",
			url:         server.URL + "/success",
			filepath:    "/root/invalid/path/file.txt",
			wantErr:     true,
			errContains: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := downloadFile(tt.filepath, tt.url)

			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)

				// Verify file content
				content, err := os.ReadFile(tt.filepath)
				require.NoError(t, err)

				require.Equal(t, tt.expectContent, string(content))
			}
		})
	}
}
