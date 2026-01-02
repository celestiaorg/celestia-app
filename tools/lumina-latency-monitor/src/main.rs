mod config;
mod keyring;
mod metrics;
mod output;
mod prometheus;
mod tx;

use std::sync::Arc;

use anyhow::Context;
use celestia_grpc::GrpcClient;
use clap::Parser;
use tokio::signal;
use tokio::sync::{Mutex, Notify};

use crate::config::{validate_args, Args, LatencyMonitorError, ValidatedConfig};
use crate::output::output_results;
use crate::tx::{run_submission_loop, TxResult};

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    let args = Args::parse();
    let config = validate_args(&args)?;

    print_startup_info(&config);

    let client = create_grpc_client(&config)?;
    let client = Arc::new(client);
    let config = Arc::new(config);

    let results: Arc<Mutex<Vec<TxResult>>> = Arc::new(Mutex::new(Vec::new()));
    let shutdown = Arc::new(Notify::new());

    // Start Prometheus metrics server if port is non-zero
    if config.metrics_port > 0 {
        let metrics_port = config.metrics_port;
        tokio::spawn(async move {
            if let Err(e) = prometheus::start_metrics_server(metrics_port).await {
                eprintln!("Prometheus metrics server error: {}", e);
            }
        });
    }

    println!("Submitting transactions...");

    let shutdown_for_signal = shutdown.clone();
    tokio::spawn(async move {
        if wait_for_shutdown().await.is_ok() {
            shutdown_for_signal.notify_one();
        }
    });

    run_submission_loop(
        client.clone(),
        config.clone(),
        results.clone(),
        shutdown.clone(),
    )
    .await;

    println!("\nStopping...");

    if !config.disable_metrics {
        let results = results.lock().await;
        output_results(&results)?;
    }

    Ok(())
}

fn create_grpc_client(config: &ValidatedConfig) -> config::Result<GrpcClient> {
    println!("Connecting to gRPC endpoint: {}", config.grpc_url);

    let client = GrpcClient::builder()
        .url(&config.grpc_url)
        .private_key_hex(&config.private_key)
        .build()
        .map_err(|e| LatencyMonitorError::GrpcClientError(e.to_string()))?;

    Ok(client)
}

fn print_startup_info(config: &ValidatedConfig) {
    println!(
        "Monitoring latency with min blob size: {} bytes, max blob size: {} bytes, \
        submission delay: {:?}",
        config.blob_size_min, config.blob_size_max, config.submission_delay,
    );
    println!("Endpoint: {}", config.grpc_url);
    println!(
        "Using account: {} ({})",
        config.account_name, config.account_address
    );
    if config.metrics_port > 0 {
        println!(
            "Prometheus metrics: http://0.0.0.0:{}/metrics",
            config.metrics_port
        );
    }
    println!("Press Ctrl+C to stop\n");
}

async fn wait_for_shutdown() -> anyhow::Result<()> {
    signal::ctrl_c()
        .await
        .context("failed to listen for shutdown signal")
}
