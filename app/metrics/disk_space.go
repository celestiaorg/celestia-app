package metrics

import (
	"os"
	"path/filepath"
	"sync"
	"time"

	"cosmossdk.io/log"
	"github.com/prometheus/client_golang/prometheus"
)

var (
	// diskSpaceBytes tracks the disk space used by each database directory
	diskSpaceBytes *prometheus.GaugeVec
	once           sync.Once
	logger         log.Logger
)

// InitDiskSpaceMetrics initializes and starts the disk space metrics collection.
// It should be called once during application startup.
func InitDiskSpaceMetrics(dataDir string, appLogger log.Logger) error {
	var initErr error
	once.Do(func() {
		logger = appLogger.With("module", "disk_space_metrics")

		// Register the gauge metric with labels for each database
		diskSpaceBytes = prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "celestia_app_disk_space_bytes",
				Help: "Disk space used by celestia-app database directories in bytes",
			},
			[]string{"database"},
		)

		// Register with the default prometheus registry (used by CometBFT)
		if err := prometheus.DefaultRegisterer.Register(diskSpaceBytes); err != nil {
			initErr = err
			// Don't start the goroutine if registration fails
			diskSpaceBytes = nil // Ensure it's nil to prevent accidental use
			return
		}

		// Start the background goroutine to update metrics every 15 seconds
		// Only start if registration succeeded
		go updateDiskSpaceMetrics(dataDir)
	})

	return initErr
}

// updateDiskSpaceMetrics periodically calculates and updates disk space metrics
func updateDiskSpaceMetrics(dataDir string) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	// Update immediately on startup
	updateMetrics(dataDir)

	// Then update every 15 seconds
	for range ticker.C {
		updateMetrics(dataDir)
	}
}

// updateMetrics calculates the size of each database directory and updates the metrics
func updateMetrics(dataDir string) {
	// Safety check: don't update if diskSpaceBytes is nil (registration failed)
	if diskSpaceBytes == nil {
		logger.Debug("Disk space metrics not initialized, skipping update")
		return
	}

	databases := []string{"blockstore.db", "state.db", "application.db"}

	for _, db := range databases {
		dbPath := filepath.Join(dataDir, "data", db)
		size, err := calculateDirSize(dbPath)
		if err != nil {
			// Log error but don't crash - the directory might not exist yet
			logger.Debug("Failed to calculate disk space", "database", db, "error", err)
			// Set metric to 0 if we can't calculate
			diskSpaceBytes.WithLabelValues(db).Set(0)
			continue
		}
		diskSpaceBytes.WithLabelValues(db).Set(float64(size))
	}
}

// calculateDirSize recursively calculates the total size of a directory
func calculateDirSize(dirPath string) (int64, error) {
	var size int64

	err := filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			// If we can't access a file/directory, log it but continue
			if !os.IsNotExist(err) {
				logger.Debug("Error accessing path during disk space calculation", "path", path, "error", err)
			}
			return nil
		}

		if !info.IsDir() {
			size += info.Size()
		}
		return nil
	})

	return size, err
}
