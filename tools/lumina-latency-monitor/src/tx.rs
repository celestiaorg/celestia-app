use std::sync::Arc;
use std::time::{Duration, Instant, SystemTime};

use celestia_grpc::{GrpcClient, TxConfig};
use celestia_types::nmt::Namespace;
use celestia_types::{AppVersion, Blob};
use rand::Rng;
use tokio::sync::{Mutex, Notify};
use tokio::task::JoinSet;
use tokio::time;

use crate::config::{LatencyMonitorError, Result, ValidatedConfig};

#[derive(Debug, Clone)]
pub struct TxResult {
    pub submit_time: SystemTime,
    pub commit_time: SystemTime,
    pub latency: Duration,
    pub tx_hash: String,
    pub height: i64,
    pub failed: bool,
    pub error_msg: String,
}

impl TxResult {
    pub fn success(
        submit_time: SystemTime,
        commit_time: SystemTime,
        latency: Duration,
        tx_hash: String,
        height: i64,
    ) -> Self {
        Self {
            submit_time,
            commit_time,
            latency,
            tx_hash,
            height,
            failed: false,
            error_msg: String::new(),
        }
    }

    pub fn failure(submit_time: SystemTime, error_msg: String) -> Self {
        Self {
            submit_time,
            commit_time: SystemTime::now(),
            latency: Duration::ZERO,
            tx_hash: String::new(),
            height: 0,
            failed: true,
            error_msg,
        }
    }
}

pub async fn run_submission_loop(
    client: Arc<GrpcClient>,
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
                let client = client.clone();
                let namespace = config.namespace;
                let size_min = config.blob_size_min;
                let size_max = config.blob_size_max;
                let disable_metrics = config.disable_metrics;
                let results = results.clone();

                tasks.spawn(async move {
                    let result = submit_transaction(&client, namespace, size_min, size_max).await;
                    if !disable_metrics {
                        results.lock().await.push(result);
                    }
                });
            }
            _ = status_ticker.tick() => {
                println!("Transactions submitted: {}", counter);
            }
            _ = shutdown.notified() => {
                break;
            }
        }
    }

    // Wait for all in-flight tasks to complete
    println!(
        "Waiting for {} in-flight transactions to complete...",
        tasks.len()
    );
    while tasks.join_next().await.is_some() {}
}

async fn submit_transaction(
    client: &GrpcClient,
    namespace: Namespace,
    size_min: usize,
    size_max: usize,
) -> TxResult {
    let submit_time = SystemTime::now();
    let submit_instant = Instant::now();

    let blob = match generate_random_blob(namespace, size_min, size_max) {
        Ok(b) => b,
        Err(e) => return TxResult::failure(submit_time, e.to_string()),
    };

    match client.submit_blobs(&[blob], TxConfig::default()).await {
        Ok(tx_info) => {
            let commit_time = SystemTime::now();
            let latency = submit_instant.elapsed();

            println!(
                "[CONFIRM] tx={} height={} latency={}ms",
                &tx_info.hash.to_string()[..16],
                tx_info.height,
                latency.as_millis()
            );

            TxResult::success(
                submit_time,
                commit_time,
                latency,
                tx_info.hash.to_string(),
                tx_info.height.value() as i64,
            )
        }
        Err(e) => {
            eprintln!("[FAILED] error={}", e);
            TxResult::failure(submit_time, e.to_string())
        }
    }
}

fn generate_random_blob(namespace: Namespace, size_min: usize, size_max: usize) -> Result<Blob> {
    let (size, data) = generate_random_data(size_min, size_max);

    println!(
        "[SUBMIT] (pending) size={} bytes time={}",
        size,
        format_time_only(SystemTime::now())
    );

    Blob::new(namespace, data, None, AppVersion::latest())
        .map_err(|e| LatencyMonitorError::BlobError(e.to_string()))
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
