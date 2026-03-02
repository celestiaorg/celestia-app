use std::sync::Arc;
use std::time::{Duration, SystemTime};

use celestia_grpc::{GrpcClient, TxConfig};
use celestia_types::nmt::Namespace;
use celestia_types::{AppVersion, Blob};
use rand::Rng;
use tokio::sync::{Mutex, Notify, Semaphore};
use tokio::task::JoinSet;
use tokio::time;

use crate::config::{LatencyMonitorError, Result, ValidatedConfig};
use crate::prometheus as prom;

#[derive(Debug, Clone)]
pub struct TxResult {
    pub submit_time: SystemTime,
    pub commit_time: SystemTime,
    pub latency: Duration,
    pub tx_hash: String,
    pub code: u32,
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
        code: u32,
        height: i64,
    ) -> Self {
        Self {
            submit_time,
            commit_time,
            latency,
            tx_hash,
            code,
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
            code: 0,
            height: 0,
            failed: true,
            error_msg,
        }
    }
}

const MAX_IN_FLIGHT: usize = 100;

pub async fn run_submission_loop(
    client: Arc<GrpcClient>,
    config: Arc<ValidatedConfig>,
    results: Arc<Mutex<Vec<TxResult>>>,
    shutdown: Arc<Notify>,
) {
    let mut submission_ticker = time::interval(config.submission_delay);
    let mut status_ticker = time::interval(Duration::from_secs(10));
    let mut counter = 0u64;
    let mut confirm_tasks: JoinSet<Option<TxResult>> = JoinSet::new();
    let sem = Arc::new(Semaphore::new(MAX_IN_FLIGHT));

    loop {
        tokio::select! {
            _ = submission_ticker.tick() => {
                counter += 1;
                let namespace = config.namespace;
                let size_min = config.blob_size_min;
                let size_max = config.blob_size_max;
                let disable_metrics = config.disable_metrics;

                let (size, blob) = match generate_random_blob(namespace, size_min, size_max) {
                    Ok(b) => b,
                    Err(e) => {
                        eprintln!("Failed to create blob: {}", e);
                        continue;
                    }
                };

                let submit_time = SystemTime::now();

                let checktx_start = std::time::Instant::now();
                let submitted = match client.broadcast_blobs(&[blob], TxConfig::default()).await {
                    Ok(s) => s,
                    Err(e) => {
                        eprintln!("[BROADCAST_FAILED] error={}", e);
                        prom::record_broadcast_failure();
                        continue;
                    }
                };
                let checktx_latency = checktx_start.elapsed();

                let tx_hash = submitted.tx_ref().hash.to_string();
                println!(
                    "[SUBMIT] tx={} size={} bytes time={}",
                    &tx_hash[..16],
                    size,
                    format_time_only(submit_time)
                );

                prom::record_checktx_latency(checktx_latency);
                prom::record_submit();

                if disable_metrics {
                    continue;
                }
                let sema_start = std::time::Instant::now();
                let permit = sem.clone().acquire_owned().await.unwrap();
                let sema_wait = sema_start.elapsed();

                let blob_size = size;
                confirm_tasks.spawn(async move {
                    let _permit = permit;
                    let tx_hash_short = tx_hash.clone();
                    match submitted.confirm().await {
                        Ok(tx_info) => {
                            let commit_time = SystemTime::now();
                            let total_latency = commit_time
                                .duration_since(submit_time)
                                .unwrap_or(Duration::ZERO);
                            let latency = total_latency.saturating_sub(sema_wait);
                            println!(
                                "[CONFIRM] tx={} height={} latency={}ms code={} time={}",
                                &tx_info.hash.to_string()[..16],
                                tx_info.height,
                                latency.as_millis(),
                                0,
                                format_time_only(commit_time)
                            );

                            prom::record_confirm(latency, blob_size);

                            Some(TxResult::success(
                                submit_time,
                                commit_time,
                                latency,
                                tx_info.hash.to_string(),
                                0,
                                tx_info.height as i64,
                            ))
                        }
                        Err(e) => {
                            let error_str = e.to_string();
                            if error_str.contains("cancel") {
                                prom::dec_in_flight();
                                println!(
                                    "[CANCELLED] tx={} context closed before confirmation",
                                    &tx_hash_short[..16]
                                );
                                return None;
                            }

                            prom::record_confirm_failure();
                            println!("[FAILED] tx={} error={}", &tx_hash_short[..16], e);
                            Some(TxResult::failure(submit_time, error_str))
                        }
                    }
                });
            }
            Some(result) = confirm_tasks.join_next() => {
                if let Ok(Some(tx_result)) = result {
                    results.lock().await.push(tx_result);
                }
            }
            _ = status_ticker.tick() => {
                println!("Transactions submitted: {}", counter);
            }
            _ = shutdown.notified() => {
                break;
            }
        }
    }

    println!(
        "Waiting for {} in-flight confirmations to complete...",
        confirm_tasks.len()
    );
    while let Some(result) = confirm_tasks.join_next().await {
        if let Ok(Some(tx_result)) = result {
            results.lock().await.push(tx_result);
        }
    }
}

fn generate_random_blob(
    namespace: Namespace,
    size_min: usize,
    size_max: usize,
) -> Result<(usize, Blob)> {
    let (size, data) = generate_random_data(size_min, size_max);

    let blob = Blob::new(namespace, data, None, AppVersion::latest())
        .map_err(|e| LatencyMonitorError::BlobError(e.to_string()))?;

    Ok((size, blob))
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
