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
use tracing_subscriber::EnvFilter;
use tokio::signal;
use tokio::sync::{Mutex, Notify};

use crate::config::{validate_args, Args, LatencyMonitorError, ValidatedConfig};
use crate::output::output_results;
use crate::tx::{run_submission_loop, TxResult};

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    tracing_subscriber::fmt()
        .with_env_filter(
            EnvFilter::from_default_env()
                .add_directive("celestia_grpc::tx_client_v2=debug".parse().unwrap()),
        )
        .init();
    let args = Args::parse();
    let config = validate_args(&args)?;

    print_startup_info(&config);

    let clients = create_grpc_clients(&config)?;
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

    let res = run_submission_loop(clients, config.clone(), results.clone(), shutdown.clone()).await;
    match res {
        Ok(_) => {
            println!("All transactions submitted successfully");
        }
        Err(e) => {
            eprintln!("Error submitting transactions: {}", e);
        }
    }

    println!("\nStopping...");

    if !config.disable_metrics {
        let results = results.lock().await;
        output_results(&results)?;
    }

    Ok(())
}

fn create_grpc_clients(config: &ValidatedConfig) -> config::Result<Vec<(Arc<str>, GrpcClient)>> {
    let mut clients = Vec::with_capacity(config.grpc_urls.len());
    for (idx, grpc_url) in config.grpc_urls.iter().enumerate() {
        println!("Connecting to gRPC endpoint {}: {}", idx + 1, grpc_url);
        let client = GrpcClient::builder()
            .url(grpc_url)
            .private_key_hex(&config.private_key)
            .build()
            .map_err(|e| LatencyMonitorError::GrpcClientError(e.to_string()))?;
        let node_id: Arc<str> = Arc::from(format!("node-{}@{}", idx + 1, grpc_url));
        clients.push((node_id, client));
    }
    Ok(clients)
}

fn print_startup_info(config: &ValidatedConfig) {
    println!(
        "Monitoring latency with min blob size: {} bytes, max blob size: {} bytes, \
        submission delay: {:?}",
        config.blob_size_min, config.blob_size_max, config.submission_delay,
    );
    for (idx, grpc_url) in config.grpc_urls.iter().enumerate() {
        if idx == 0 {
            println!("Endpoint 1 (primary): {}", grpc_url);
        } else {
            println!("Endpoint {}: {}", idx + 1, grpc_url);
        }
    }
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
