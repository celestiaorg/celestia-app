package fibre

import (
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"time"

	pebbledb "github.com/cockroachdb/pebble/v2"
	"github.com/cockroachdb/pebble/v2/vfs"
	"github.com/cockroachdb/pebble/v2/vfs/atomicfs"
)

const storageBackendInvalid = "invalid"

// StoreInspector reads store metadata without opening shard backends or modifying the store.
type StoreInspector struct {
	db *pebbledb.DB
}

// MarkerInfo describes one shard marker in the store.
type MarkerInfo struct {
	Key         string
	Commitment  string
	PromiseHash string
	Backend     string
	Size        int64
	Legacy      bool
	Error       string
}

// MarkerFilter limits the markers returned by [StoreInspector.Markers].
// Backend may be empty, "local", or "object". Limit zero returns all matches.
type MarkerFilter struct {
	Backend string
	Limit   int
}

// MarkerPage contains matching markers and reports whether more matches exist.
type MarkerPage struct {
	Markers   []MarkerInfo
	Truncated bool
}

// StoreStats summarizes shard markers and the prune backlog.
type StoreStats struct {
	Markers          int // includes valid and invalid markers
	LocalMarkers     int
	ObjectMarkers    int
	LegacyMarkers    int
	InvalidMarkers   int
	LocalBytes       uint64 // excludes legacy markers, which record no size
	ObjectBytes      uint64 // recorded marker sizes
	PruneEntries     int
	InvalidPruneKeys int
	PruneBacklog     int // valid entries earlier than the current UTC minute
	OldestPruneAt    time.Time
	ObjectNamespace  *ObjectNamespaceInfo
	NamespaceError   string
}

// ObjectNamespaceInfo identifies the saved object-storage namespace.
type ObjectNamespaceInfo struct {
	Endpoint         string
	Bucket           string
	Prefix           string
	ChainID          string
	ValidatorAddress string
}

// OpenStoreInspector opens an existing Fibre store in Pebble read-only mode.
func OpenStoreInspector(path string) (*StoreInspector, error) {
	if path == "" {
		return nil, fmt.Errorf("store path is required")
	}
	exists, err := pebbleStoreExists(path)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, fmt.Errorf("no Fibre store found at %s", path)
	}
	db, err := pebbledb.Open(path, &pebbledb.Options{ReadOnly: true})
	if err != nil {
		return nil, fmt.Errorf("opening store for inspection; stop the Fibre server first: %w", err)
	}
	return &StoreInspector{db: db}, nil
}

func pebbleStoreExists(path string) (bool, error) {
	manifest, err := atomicfs.ReadMarker(vfs.Default, path, "manifest")
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("checking Fibre store at %s: %w", path, err)
	}
	if manifest == "" {
		return false, nil
	}
	_, err = vfs.Default.Stat(vfs.Default.PathJoin(path, manifest))
	if os.IsNotExist(err) {
		return false, nil
	}
	return err == nil, err
}

// Close closes the read-only store handle.
func (i *StoreInspector) Close() error {
	return i.db.Close()
}

// Markers returns shard markers in Pebble key order.
func (i *StoreInspector) Markers(ctx context.Context, filter MarkerFilter) (MarkerPage, error) {
	if filter.Backend != "" && filter.Backend != storageBackendLocal && filter.Backend != storageBackendObject && filter.Backend != storageBackendInvalid {
		return MarkerPage{}, fmt.Errorf("backend must be local, object, or invalid")
	}
	if filter.Limit < 0 {
		return MarkerPage{}, fmt.Errorf("limit must not be negative")
	}

	var page MarkerPage
	err := i.scanMarkers(ctx, func(marker MarkerInfo) bool {
		if filter.Backend != "" && marker.Backend != filter.Backend {
			return true
		}
		if filter.Limit > 0 && len(page.Markers) == filter.Limit {
			page.Truncated = true
			return false
		}
		page.Markers = append(page.Markers, marker)
		return true
	})
	if err != nil {
		return MarkerPage{}, err
	}
	return page, nil
}

// Stats scans the store metadata and reports marker and prune-index totals.
func (i *StoreInspector) Stats(ctx context.Context, now time.Time) (StoreStats, error) {
	var stats StoreStats
	err := i.scanMarkers(ctx, func(marker MarkerInfo) bool {
		stats.Markers++
		switch marker.Backend {
		case storageBackendLocal:
			stats.LocalMarkers++
			stats.LocalBytes += uint64(marker.Size)
			if marker.Legacy {
				stats.LegacyMarkers++
			}
		case storageBackendObject:
			stats.ObjectMarkers++
			stats.ObjectBytes += uint64(marker.Size)
		default:
			stats.InvalidMarkers++
		}
		return true
	})
	if err != nil {
		return stats, err
	}

	if err := i.scanPruneStats(ctx, now, &stats); err != nil {
		return stats, err
	}
	namespace, recorded, err := readObjectNamespace(i.db)
	if err != nil {
		stats.NamespaceError = err.Error()
	} else if recorded {
		stats.ObjectNamespace = &ObjectNamespaceInfo{
			Endpoint: namespace.Endpoint, Bucket: namespace.Bucket, Prefix: namespace.Prefix,
			ChainID: namespace.ChainID, ValidatorAddress: namespace.ValidatorAddress,
		}
	}
	return stats, nil
}

func (i *StoreInspector) scanMarkers(ctx context.Context, visit func(MarkerInfo) bool) error {
	prefix := []byte(shardKeyPrefix)
	iter, err := i.db.NewIter(&pebbledb.IterOptions{LowerBound: prefix, UpperBound: prefixUpperBound(prefix)})
	if err != nil {
		return fmt.Errorf("creating shard iterator: %w", err)
	}
	defer iter.Close()

	for valid := iter.First(); valid; valid = iter.Next() {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !visit(inspectMarker(iter.Key(), iter.Value())) {
			break
		}
	}
	if err := iter.Error(); err != nil {
		return fmt.Errorf("iterating shard markers: %w", err)
	}
	return nil
}

func inspectMarker(key, value []byte) MarkerInfo {
	marker := MarkerInfo{Key: string(key)}
	commitment, promiseHash, ok := parseShardKey(marker.Key)
	if !ok {
		marker.Backend = storageBackendInvalid
		marker.Error = "invalid shard key"
		return marker
	}
	marker.Commitment = commitment.String()
	marker.PromiseHash = hex.EncodeToString(promiseHash)
	backend, size, err := decodeShardMarkerBackend(value)
	if err != nil {
		marker.Backend = storageBackendInvalid
		marker.Error = err.Error()
		return marker
	}
	marker.Size = size
	marker.Legacy = len(value) == 0
	switch backend {
	case localBackendTag:
		marker.Backend = storageBackendLocal
	case objectBackendTag:
		marker.Backend = storageBackendObject
	}
	return marker
}

func (i *StoreInspector) scanPruneStats(ctx context.Context, now time.Time, stats *StoreStats) error {
	prefix := []byte("/prune/")
	iter, err := i.db.NewIter(&pebbledb.IterOptions{LowerBound: prefix, UpperBound: prefixUpperBound(prefix)})
	if err != nil {
		return fmt.Errorf("creating prune iterator: %w", err)
	}
	defer iter.Close()

	cutoff := now.UTC().Truncate(time.Minute)
	for valid := iter.First(); valid; valid = iter.Next() {
		if err := ctx.Err(); err != nil {
			return err
		}
		stats.PruneEntries++
		pruneAt, ok := parsePruneTime(string(iter.Key()))
		if !ok {
			stats.InvalidPruneKeys++
			continue
		}
		if pruneAt.Before(cutoff) {
			stats.PruneBacklog++
			if stats.OldestPruneAt.IsZero() {
				stats.OldestPruneAt = pruneAt
			}
		}
	}
	if err := iter.Error(); err != nil {
		return fmt.Errorf("iterating prune index: %w", err)
	}
	return nil
}

func parsePruneTime(key string) (time.Time, bool) {
	suffix, ok := strings.CutPrefix(key, "/prune/")
	if !ok {
		return time.Time{}, false
	}
	timestamp, suffix, ok := strings.Cut(suffix, "/")
	if !ok {
		return time.Time{}, false
	}
	commitment, promiseHash, ok := strings.Cut(suffix, "/")
	if !ok || strings.Contains(promiseHash, "/") {
		return time.Time{}, false
	}
	if _, err := CommitmentFromString(commitment); err != nil {
		return time.Time{}, false
	}
	if _, err := hex.DecodeString(promiseHash); err != nil {
		return time.Time{}, false
	}
	pruneAt, err := time.ParseInLocation("200601021504", timestamp, time.UTC)
	return pruneAt, err == nil
}
