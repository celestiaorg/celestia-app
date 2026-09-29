package appd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// staleSuffix marks a hidden directory that is being removed.
const staleSuffix = ".stale"

// PruneStaleBinaries removes extracted binary directories whose version is not
// in keep and returns the removed paths. Hidden entries and symlinks are skipped.
// Each directory is renamed to a hidden path before removal so a partial removal
// never looks like a complete extraction.
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
		name := entry.Name()
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(dir, name)
		staged := path
		if strings.HasPrefix(name, ".") {
			if !strings.HasSuffix(name, staleSuffix) {
				continue
			}
		} else {
			if slices.Contains(keep, name) {
				continue
			}
			staged = filepath.Join(dir, "."+name+staleSuffix)
			if err := os.Rename(path, staged); err != nil {
				cleanupErr = errors.Join(cleanupErr, fmt.Errorf("rename %s: %w", path, err))
				continue
			}
		}
		if err := os.RemoveAll(staged); err != nil {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("remove %s: %w", staged, err))
			continue
		}
		removed = append(removed, path)
	}
	return removed, cleanupErr
}
