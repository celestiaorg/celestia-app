//! Sovereign SDK backend for blob submission
//!
//! Uses sov-celestia-adapter from commit 093ec3b90 which has the mutex removed,
//! enabling parallel blob submissions.

use std::sync::Arc;
use std::time::{Duration, Instant, SystemTime};

use rand::Rng;
use sov_celestia_adapter::{CelestiaConfig, CelestiaService, DaService};
use sov_celestia_adapter::verifier::RollupParams;
use tokio::sync::{Mutex, Notify};
use tokio::task::JoinSet;
use tokio::time;

use crate::config::ValidatedConfig;
use crate::tx::TxResult;

/// Create a Sovereign SDK CelestiaService
pub async fn create_sovereign_client(config: &ValidatedConfig) -> anyhow::Result<CelestiaService> {
    println!("Creating Sovereign SDK CelestiaService...");
    println!("  RPC endpoint: {}", config.rpc_url);
    println!("  gRPC endpoint: {}", config.grpc_url);
    if config.grpc_token.is_some() {
        println!("  gRPC token: (set)");
    }

    let mut celestia_config = CelestiaConfig::minimal(config.rpc_url.clone())
        .with_submission(config.grpc_url.clone(), config.private_key.clone());

    // Set gRPC auth token if provided (for QuickNode)
    if let Some(ref token) = config.grpc_token {
        celestia_config.grpc_auth_token = Some(token.clone());
    }

    let rollup_params = RollupParams {
        rollup_batch_namespace: config.namespace,
        rollup_proof_namespace: config.namespace, // Use same namespace for simplicity
    };

    Ok(CelestiaService::new(celestia_config, rollup_params).await)
}

/// Run submission loop using Sovereign SDK's CelestiaService
pub async fn run_sovereign_submission_loop(
    service: Arc<CelestiaService>,
    config: Arc<ValidatedConfig>,
    results: Arc<Mutex<Vec<TxResult>>>,
    shutdown: Arc<Notify>,
) {
    let mut submission_ticker = time::interval(config.submission_delay);
    let mut status_ticker = time::interval(Duration::from_secs(10));
    let mut counter = 0u64;
    let mut tasks = JoinSet::new();

    loop {
        tokio::select! {
            _ = submission_ticker.tick() => {
                counter += 1;
                let service = service.clone();
                let size_min = config.blob_size_min;
                let size_max = config.blob_size_max;
                let disable_metrics = config.disable_metrics;
                let results = results.clone();

                // Spawn parallel submissions - no mutex blocking!
                tasks.spawn(async move {
                    let result = submit_via_sovereign(&service, size_min, size_max).await;
                    if !disable_metrics {
                        results.lock().await.push(result);
                    }
                });
            }
            _ = status_ticker.tick() => {
                println!("[SOVEREIGN] Transactions submitted: {} (in-flight: {})", counter, tasks.len());
            }
            _ = shutdown.notified() => {
                break;
            }
        }
    }

    println!(
        "Waiting for {} in-flight transactions to complete...",
        tasks.len()
    );
    while tasks.join_next().await.is_some() {}
}

async fn submit_via_sovereign(
    service: &CelestiaService,
    size_min: usize,
    size_max: usize,
) -> TxResult {
    let submit_time = SystemTime::now();
    let submit_instant = Instant::now();

    let (size, data) = generate_random_data(size_min, size_max);

    println!(
        "[SOVEREIGN SUBMIT] (pending) size={} bytes time={}",
        size,
        format_time_only(submit_time)
    );

    // Use send_transaction which internally calls submit_blob_to_namespace
    let rx = service.send_transaction(&data).await;

    match rx.await {
        Ok(Ok(receipt)) => {
            let commit_time = SystemTime::now();
            let latency = submit_instant.elapsed();

            println!(
                "[SOVEREIGN CONFIRM] blob_hash={} latency={}ms",
                receipt.blob_hash,
                latency.as_millis()
            );

            TxResult::success(
                submit_time,
                commit_time,
                latency,
                receipt.blob_hash.to_string(),
                0, // Height not directly available in receipt
            )
        }
        Ok(Err(e)) => {
            eprintln!("[SOVEREIGN FAILED] error={}", e);
            TxResult::failure(submit_time, e.to_string())
        }
        Err(e) => {
            eprintln!("[SOVEREIGN FAILED] channel error={}", e);
            TxResult::failure(submit_time, e.to_string())
        }
    }
}

fn generate_random_data(size_min: usize, size_max: usize) -> (usize, Vec<u8>) {
    let mut rng = rand::thread_rng();
    let size = if size_max > size_min {
        rng.gen_range(size_min..=size_max)
    } else {
        size_min
    };

    let mut data = vec![0u8; size];
    rng.fill(&mut data[..]);
    (size, data)
}

fn format_time_only(time: SystemTime) -> String {
    humantime::format_rfc3339(time)
        .to_string()
        .split('T')
        .nth(1)
        .map_or_else(String::new, |s| s.to_string())
}
