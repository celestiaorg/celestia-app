package appd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// PruneStaleBinaries removes extracted binary directories whose version is not
// in keep and returns the removed paths. Hidden entries and symlinks are skipped.
func PruneStaleBinaries(keep []string) ([]string, error) {
	dir := getDirectoryForCelestiaAppBinaries()
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var removed []string
	var cleanupErr error
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") || slices.Contains(keep, entry.Name()) {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		if err := os.RemoveAll(path); err != nil {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("remove %s: %w", path, err))
			continue
		}
		removed = append(removed, path)
	}
	return removed, cleanupErr
}
