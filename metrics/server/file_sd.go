package server

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// FileSDTarget represents a single target group in Prometheus file_sd format.
// See: https://prometheus.io/docs/prometheus/latest/configuration/configuration/#file_sd_config
type FileSDTarget struct {
	Targets []string          `json:"targets"`
	Labels  map[string]string `json:"labels,omitempty"`
}

// FileSDWriter writes targets in Prometheus file_sd JSON format.
type FileSDWriter struct {
	filePath string
}

// NewFileSDWriter creates a new file_sd writer.
func NewFileSDWriter(filePath string) *FileSDWriter {
	return &FileSDWriter{
		filePath: filePath,
	}
}

// Write atomically writes the targets to the file_sd JSON file.
func (w *FileSDWriter) Write(targets []*TargetEntry) error {
	// Convert targets to file_sd format
	fileTargets := make([]FileSDTarget, 0, len(targets))
	for _, t := range targets {
		labels := make(map[string]string, len(t.Labels)+1)
		labels["node_id"] = t.NodeID
		for k, v := range t.Labels {
			labels[k] = v
		}

		fileTargets = append(fileTargets, FileSDTarget{
			Targets: []string{t.Address},
			Labels:  labels,
		})
	}

	// Marshal to JSON with indentation for readability
	data, err := json.MarshalIndent(fileTargets, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal targets: %w", err)
	}

	// Write atomically: write to temp file, then rename
	dir := filepath.Dir(w.filePath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	tmpFile := w.filePath + ".tmp"
	if err := os.WriteFile(tmpFile, data, 0o644); err != nil {
		return fmt.Errorf("failed to write temp file: %w", err)
	}

	if err := os.Rename(tmpFile, w.filePath); err != nil {
		os.Remove(tmpFile) // Clean up on failure
		return fmt.Errorf("failed to rename temp file: %w", err)
	}

	return nil
}

// Path returns the file path being written to.
func (w *FileSDWriter) Path() string {
	return w.filePath
}
