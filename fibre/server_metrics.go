package fibre

import (
	"github.com/prometheus/client_golang/prometheus"
)

// ServerMetrics holds Prometheus metrics for the Fibre server.
type ServerMetrics struct {
	// UploadShardTotal counts upload attempts by status (success|error).
	UploadShardTotal *prometheus.CounterVec
	// UploadShardDuration observes upload latency.
	UploadShardDuration prometheus.Histogram
	// UploadShardBytesTotal counts total shard bytes received.
	UploadShardBytesTotal prometheus.Counter
	// UploadShardRowsTotal counts total rows received.
	UploadShardRowsTotal prometheus.Counter
	// UploadShardsInFlight tracks currently processing uploads.
	UploadShardsInFlight prometheus.Gauge
	// DownloadShardTotal counts download attempts by status (success|error|not_found).
	DownloadShardTotal *prometheus.CounterVec
	// DownloadShardDuration observes download latency.
	DownloadShardDuration prometheus.Histogram
}

// NewServerMetrics creates a new [ServerMetrics] and registers all collectors
// with the given [prometheus.Registerer]. Pass prometheus.DefaultRegisterer for
// the global registry or a custom one for testing.
func NewServerMetrics(reg prometheus.Registerer) *ServerMetrics {
	m := &ServerMetrics{
		UploadShardTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "fibre",
			Name:      "upload_shard_total",
			Help:      "Total number of UploadShard RPCs by status.",
		}, []string{"status"}),
		UploadShardDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: "fibre",
			Name:      "upload_shard_duration_seconds",
			Help:      "Latency of UploadShard RPCs in seconds.",
			Buckets:   prometheus.DefBuckets,
		}),
		UploadShardBytesTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "fibre",
			Name:      "upload_shard_bytes_total",
			Help:      "Total shard bytes received via UploadShard.",
		}),
		UploadShardRowsTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "fibre",
			Name:      "upload_shard_rows_total",
			Help:      "Total rows received via UploadShard.",
		}),
		UploadShardsInFlight: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "fibre",
			Name:      "upload_shards_in_flight",
			Help:      "Number of UploadShard RPCs currently in progress.",
		}),
		DownloadShardTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "fibre",
			Name:      "download_shard_total",
			Help:      "Total number of DownloadShard RPCs by status.",
		}, []string{"status"}),
		DownloadShardDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: "fibre",
			Name:      "download_shard_duration_seconds",
			Help:      "Latency of DownloadShard RPCs in seconds.",
			Buckets:   prometheus.DefBuckets,
		}),
	}

	reg.MustRegister(
		m.UploadShardTotal,
		m.UploadShardDuration,
		m.UploadShardBytesTotal,
		m.UploadShardRowsTotal,
		m.UploadShardsInFlight,
		m.DownloadShardTotal,
		m.DownloadShardDuration,
	)

	return m
}

// NopServerMetrics returns a [ServerMetrics] whose collectors are not
// registered with any registry. The metrics still work (no panics), but
// the values are never scraped. Use this when metrics are disabled.
func NopServerMetrics() *ServerMetrics {
	return &ServerMetrics{
		UploadShardTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "fibre",
			Name:      "upload_shard_total",
			Help:      "Total number of UploadShard RPCs by status.",
		}, []string{"status"}),
		UploadShardDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: "fibre",
			Name:      "upload_shard_duration_seconds",
			Help:      "Latency of UploadShard RPCs in seconds.",
		}),
		UploadShardBytesTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "fibre",
			Name:      "upload_shard_bytes_total",
			Help:      "Total shard bytes received via UploadShard.",
		}),
		UploadShardRowsTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "fibre",
			Name:      "upload_shard_rows_total",
			Help:      "Total rows received via UploadShard.",
		}),
		UploadShardsInFlight: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "fibre",
			Name:      "upload_shards_in_flight",
			Help:      "Number of UploadShard RPCs currently in progress.",
		}),
		DownloadShardTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "fibre",
			Name:      "download_shard_total",
			Help:      "Total number of DownloadShard RPCs by status.",
		}, []string{"status"}),
		DownloadShardDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: "fibre",
			Name:      "download_shard_duration_seconds",
			Help:      "Latency of DownloadShard RPCs in seconds.",
		}),
	}
}
