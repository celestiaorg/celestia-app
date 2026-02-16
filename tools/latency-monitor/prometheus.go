package main

import (
	"fmt"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	// txSubmitted is the total number of transactions submitted (broadcast succeeded).
	txSubmitted = promauto.NewCounter(prometheus.CounterOpts{
		Name: "latency_monitor_tx_submitted_total",
		Help: "Total number of transactions submitted",
	})

	// txConfirmed is the total number of transactions successfully confirmed.
	txConfirmed = promauto.NewCounter(prometheus.CounterOpts{
		Name: "latency_monitor_tx_confirmed_total",
		Help: "Total number of transactions confirmed",
	})

	// txBroadcastFailed is the total number of transactions that failed to broadcast.
	txBroadcastFailed = promauto.NewCounter(prometheus.CounterOpts{
		Name: "latency_monitor_tx_broadcast_failed_total",
		Help: "Total number of transactions that failed to broadcast",
	})

	// txConfirmFailed is the total number of transactions that failed confirmation.
	txConfirmFailed = promauto.NewCounter(prometheus.CounterOpts{
		Name: "latency_monitor_tx_confirm_failed_total",
		Help: "Total number of transactions that failed confirmation",
	})

	// txParallelSubmissionFailed is the total number of parallel submission failures.
	txParallelSubmissionFailed = promauto.NewCounter(prometheus.CounterOpts{
		Name: "latency_monitor_tx_parallel_submission_failed_total",
		Help: "Total number of parallel submission failures (includes both broadcast and confirmation failures)",
	})

	// txInFlight is the number of transactions currently in-flight (submitted but not yet confirmed).
	txInFlight = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "latency_monitor_tx_in_flight",
		Help: "Number of transactions in flight",
	})

	// bytesConfirmed is the total number of bytes confirmed (for throughput calculation).
	bytesConfirmed = promauto.NewCounter(prometheus.CounterOpts{
		Name: "latency_monitor_bytes_confirmed_total",
		Help: "Total number of bytes confirmed",
	})

	// latencyHistogram is a histogram of transaction latencies in seconds.
	latencyHistogram = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "latency_monitor_latency_seconds",
		Help:    "Transaction latency in seconds",
		Buckets: []float64{0.5, 1.0, 2.0, 5.0, 10.0, 15.0, 20.0, 30.0, 45.0, 60.0, 90.0, 120.0},
	})

	// checkTxLatencyHistogram measures broadcast->CheckTx time in seconds.
	checkTxLatencyHistogram = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "latency_monitor_checktx_seconds",
		Help:    "Time for BroadcastTx to return (mempool CheckTx acceptance) in seconds",
		Buckets: []float64{0.01, 0.025, 0.05, 0.1, 0.2, 0.35, 0.5, 0.75, 1.0, 2.0, 5.0},
	})
)

// recordSubmit records a transaction submission.
func recordSubmit() {
	txSubmitted.Inc()
	txInFlight.Inc()
}

// recordConfirm records a successful transaction confirmation with its latency and blob size.
func recordConfirm(latency time.Duration, blobSize int) {
	txConfirmed.Inc()
	txInFlight.Dec()
	bytesConfirmed.Add(float64(blobSize))
	latencyHistogram.Observe(latency.Seconds())
}

// recordCheckTxLatency records how long BroadcastTx took to return.
func recordCheckTxLatency(latency time.Duration) {
	checkTxLatencyHistogram.Observe(latency.Seconds())
}

// recordBroadcastFailure records a broadcast failure.
// Does NOT decrement in-flight since the tx was never successfully submitted.
func recordBroadcastFailure() {
	txBroadcastFailed.Inc()
}

// recordConfirmFailure records a confirmation failure.
// Decrements in-flight since the tx was previously submitted.
func recordConfirmFailure() {
	txConfirmFailed.Inc()
	txInFlight.Dec()
}

// recordParallelSubmissionFailure records a parallel submission failure.
// Used when parallel worker submission fails (broadcast or confirmation).
// Decrements in-flight since the tx was previously queued.
func recordParallelSubmissionFailure() {
	txParallelSubmissionFailed.Inc()
	txInFlight.Dec()
}

// startObservabilityServer starts the Prometheus observability HTTP server on the given port.
func startObservabilityServer(port int) {
	addr := fmt.Sprintf(":%d", port)
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())

	server := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	fmt.Printf("Prometheus metrics server listening on http://0.0.0.0%s/metrics\n", addr)

	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Printf("Observability server error: %v\n", err)
		}
	}()
}
