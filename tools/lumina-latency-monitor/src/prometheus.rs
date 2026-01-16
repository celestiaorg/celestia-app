//! Prometheus metrics server for the latency monitor.

use std::convert::Infallible;
use std::net::SocketAddr;
use std::time::Duration;

use http_body_util::Full;
use hyper::body::Bytes;
use hyper::server::conn::http1;
use hyper::service::service_fn;
use hyper::{Request, Response, StatusCode};
use hyper_util::rt::TokioIo;
use lazy_static::lazy_static;
use prometheus::{
    register_counter, register_gauge, register_histogram, Counter, Encoder, Gauge, Histogram,
    TextEncoder,
};
use tokio::net::TcpListener;

lazy_static! {
    /// Total number of transactions submitted (broadcast succeeded).
    pub static ref TX_SUBMITTED: Counter = register_counter!(
        "latency_monitor_tx_submitted_total",
        "Total number of transactions submitted"
    )
    .unwrap();

    /// Total number of transactions successfully confirmed.
    pub static ref TX_CONFIRMED: Counter = register_counter!(
        "latency_monitor_tx_confirmed_total",
        "Total number of transactions confirmed"
    )
    .unwrap();

    /// Total number of transactions that failed to broadcast.
    pub static ref TX_BROADCAST_FAILED: Counter = register_counter!(
        "latency_monitor_tx_broadcast_failed_total",
        "Total number of transactions that failed to broadcast"
    )
    .unwrap();

    /// Total number of transactions that failed confirmation.
    pub static ref TX_CONFIRM_FAILED: Counter = register_counter!(
        "latency_monitor_tx_confirm_failed_total",
        "Total number of transactions that failed confirmation"
    )
    .unwrap();

    /// Number of transactions currently in-flight (submitted but not yet confirmed).
    pub static ref TX_IN_FLIGHT: Gauge = register_gauge!(
        "latency_monitor_tx_in_flight",
        "Number of transactions in flight"
    )
    .unwrap();

    /// Total number of bytes confirmed (for throughput calculation).
    pub static ref BYTES_CONFIRMED: Counter = register_counter!(
        "latency_monitor_bytes_confirmed_total",
        "Total number of bytes confirmed"
    )
    .unwrap();

    /// Histogram of transaction latencies in seconds.
    pub static ref LATENCY_HISTOGRAM: Histogram = register_histogram!(
        "latency_monitor_latency_seconds",
        "Transaction latency in seconds",
        vec![0.5, 1.0, 2.0, 5.0, 10.0, 15.0, 20.0, 30.0, 45.0, 60.0, 90.0, 120.0]
    )
    .unwrap();

    /// Histogram of CheckTx (BroadcastTx) latencies in seconds.
    pub static ref CHECKTX_HISTOGRAM: Histogram = register_histogram!(
        "latency_monitor_checktx_seconds",
        "Time for BroadcastTx to return (mempool CheckTx acceptance) in seconds",
        vec![0.01, 0.025, 0.05, 0.1, 0.2, 0.35, 0.5, 0.75, 1.0, 2.0, 5.0]
    )
    .unwrap();
}

/// Record a transaction submission.
pub fn record_submit() {
    TX_SUBMITTED.inc();
    TX_IN_FLIGHT.inc();
}

/// Record a successful transaction confirmation with its latency and blob size.
pub fn record_confirm(latency: Duration, blob_size: usize) {
    TX_CONFIRMED.inc();
    TX_IN_FLIGHT.dec();
    BYTES_CONFIRMED.inc_by(blob_size as f64);
    LATENCY_HISTOGRAM.observe(latency.as_secs_f64());
}

/// Record how long BroadcastTx took to return.
pub fn record_checktx_latency(latency: Duration) {
    CHECKTX_HISTOGRAM.observe(latency.as_secs_f64());
}

/// Record a broadcast failure.
/// Does NOT decrement in-flight since the tx was never successfully submitted.
pub fn record_broadcast_failure() {
    TX_BROADCAST_FAILED.inc();
}

/// Record a confirmation failure.
/// Decrements in-flight since the tx was previously submitted.
pub fn record_confirm_failure() {
    TX_CONFIRM_FAILED.inc();
    TX_IN_FLIGHT.dec();
}

/// Decrement in-flight counter for cancelled transactions.
pub fn dec_in_flight() {
    TX_IN_FLIGHT.dec();
}

/// Handle HTTP requests - serve metrics on /metrics.
async fn handle_request(
    req: Request<hyper::body::Incoming>,
) -> Result<Response<Full<Bytes>>, Infallible> {
    if req.uri().path() == "/metrics" {
        let encoder = TextEncoder::new();
        let metric_families = prometheus::gather();
        let mut buffer = Vec::new();
        encoder.encode(&metric_families, &mut buffer).unwrap();

        Ok(Response::builder()
            .status(StatusCode::OK)
            .header("Content-Type", encoder.format_type())
            .body(Full::new(Bytes::from(buffer)))
            .unwrap())
    } else {
        Ok(Response::builder()
            .status(StatusCode::NOT_FOUND)
            .body(Full::new(Bytes::from("Not Found")))
            .unwrap())
    }
}

/// Start the Prometheus metrics HTTP server.
pub async fn start_metrics_server(port: u16) -> anyhow::Result<()> {
    let addr = SocketAddr::from(([0, 0, 0, 0], port));
    let listener = TcpListener::bind(addr).await?;
    println!(
        "Prometheus metrics server listening on http://{}/metrics",
        addr
    );

    loop {
        let (stream, _) = listener.accept().await?;
        let io = TokioIo::new(stream);

        tokio::spawn(async move {
            if let Err(err) = http1::Builder::new()
                .serve_connection(io, service_fn(handle_request))
                .await
            {
                eprintln!("Error serving connection: {:?}", err);
            }
        });
    }
}
