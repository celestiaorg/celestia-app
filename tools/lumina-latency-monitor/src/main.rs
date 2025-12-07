mod config;
mod keyring;
mod metrics;
mod output;
#[cfg(feature = "sovereign")]
mod sovereign;
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

    let results: Arc<Mutex<Vec<TxResult>>> = Arc::new(Mutex::new(Vec::new()));
    let shutdown = Arc::new(Notify::new());
    let config = Arc::new(config);

    println!("Submitting transactions...");

    #[cfg(feature = "sovereign")]
    let loop_handle = if config.use_sovereign {
        println!("Using Sovereign SDK backend (parallel submissions enabled)");
        let service = sovereign::create_sovereign_client(&config).await?;
        let service = Arc::new(service);
        tokio::spawn(sovereign::run_sovereign_submission_loop(
            service,
            config.clone(),
            results.clone(),
            shutdown.clone(),
        ))
    } else {
        let client = create_grpc_client(&config)?;
        let client = Arc::new(client);
        tokio::spawn(run_submission_loop(
            client,
            config.clone(),
            results.clone(),
            shutdown.clone(),
        ))
    };

    #[cfg(not(feature = "sovereign"))]
    let loop_handle = {
        let client = create_grpc_client(&config)?;
        let client = Arc::new(client);
        tokio::spawn(run_submission_loop(
            client,
            config.clone(),
            results.clone(),
            shutdown.clone(),
        ))
    };

    wait_for_shutdown().await?;
    shutdown.notify_one();

    println!("\nStopping...");
    let _ = loop_handle.await;

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
    println!("gRPC Endpoint: {}", config.grpc_url);
    println!("RPC Endpoint: {}", config.rpc_url);
    println!(
        "Using account: {} ({})",
        config.account_name, config.account_address
    );
    #[cfg(feature = "sovereign")]
    if config.use_sovereign {
        println!("Backend: Sovereign SDK (parallel submissions)");
    } else {
        println!("Backend: celestia-grpc (parallel submissions)");
    }
    #[cfg(not(feature = "sovereign"))]
    println!("Backend: celestia-grpc (parallel submissions)");
    println!("Press Ctrl+C to stop\n");
}

async fn wait_for_shutdown() -> anyhow::Result<()> {
    signal::ctrl_c()
        .await
        .context("failed to listen for shutdown signal")
}
