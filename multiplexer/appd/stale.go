package appd

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// StaleBinaries returns the extracted binary directories whose version is not
// in keep. Hidden entries (in-progress extractions) are skipped.
func StaleBinaries(keep []string) ([]string, error) {
	dir := getDirectoryForCelestiaAppBinaries()
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var stale []string
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") || slices.Contains(keep, entry.Name()) {
			continue
		}
		stale = append(stale, filepath.Join(dir, entry.Name()))
	}
	return stale, nil
}
