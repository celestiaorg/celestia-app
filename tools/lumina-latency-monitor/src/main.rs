use std::path::PathBuf;
use std::sync::Arc;
use std::time::{Duration, Instant, SystemTime};

use anyhow::Context;
use celestia_grpc::{GrpcClient, TxConfig};
use celestia_types::nmt::Namespace;
use celestia_types::{AppVersion, Blob};
use clap::Parser;
use rand::Rng;
use thiserror::Error;
use tokio::signal;
use tokio::sync::{Mutex, Notify};
use tokio::time;

#[derive(Error, Debug)]
pub enum LatencyMonitorError {
    #[error("minimum blob size must be at least 1 byte")]
    InvalidMinBlobSize,

    #[error("maximum blob size must be greater than or equal to minimum blob size")]
    InvalidBlobSizeRange,

    #[error("private key is required (use --private-key or CELESTIA_PRIVATE_KEY)")]
    MissingPrivateKey,

    #[error("private key must be a valid hex string")]
    InvalidPrivateKeyHex,

    #[error("failed to parse submission delay: {0}")]
    InvalidSubmissionDelay(String),

    #[error("failed to create gRPC client: {0}")]
    GrpcClientError(String),

    #[error("failed to create namespace: {0}")]
    NamespaceError(String),

    #[error("failed to create blob: {0}")]
    BlobError(String),

    #[error("transaction submission failed: {0}")]
    SubmissionError(String),

    #[error("CSV write error: {0}")]
    CsvError(#[from] csv::Error),

    #[error("IO error: {0}")]
    IoError(#[from] std::io::Error),
}

type Result<T> = std::result::Result<T, LatencyMonitorError>;

#[derive(Parser, Debug)]
#[command(name = "lumina-latency-monitor")]
#[command(about = "Monitor and measure transaction latency in Celestia networks")]
struct Args {
    /// gRPC endpoint(s). Can be specified multiple times for fallback support.
    #[arg(short = 'e', long, default_value = "localhost:9090")]
    grpc_endpoint: Vec<String>,

    #[arg(short = 'k', long)]
    keyring_dir: Option<PathBuf>,

    #[arg(short = 'a', long)]
    account: Option<String>,

    #[arg(short = 'p', long, env = "CELESTIA_PRIVATE_KEY")]
    private_key: Option<String>,

    #[arg(short = 'b', long, default_value_t = 1024)]
    blob_size: usize,

    #[arg(short = 'z', long, default_value_t = 1)]
    blob_size_min: usize,

    #[arg(short = 'n', long, default_value = "test")]
    namespace: String,

    #[arg(short = 'm', long)]
    disable_metrics: bool,

    #[arg(short = 'd', long, default_value = "4000ms")]
    submission_delay: String,

    #[arg(long, conflicts_with = "no_tls")]
    tls: bool,

    #[arg(long, alias = "insecure", conflicts_with = "tls")]
    no_tls: bool,
}

struct ValidatedConfig {
    grpc_urls: Vec<String>,
    private_key: String,
    blob_size_min: usize,
    blob_size_max: usize,
    namespace: String,
    submission_delay: Duration,
    disable_metrics: bool,
}

#[derive(Debug, Clone)]
struct TxResult {
    submit_time: SystemTime,
    commit_time: SystemTime,
    latency: Duration,
    tx_hash: String,
    code: u32,
    height: i64,
    failed: bool,
    error_msg: String,
}

impl TxResult {
    fn success(
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
            code: 0,
            height,
            failed: false,
            error_msg: String::new(),
        }
    }

    fn failure(submit_time: SystemTime, error_msg: String) -> Self {
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

#[derive(Debug, Default)]
struct Statistics {
    total_count: usize,
    success_count: usize,
    failure_count: usize,
    mean_latency_ms: f64,
    std_dev_ms: f64,
}

impl Statistics {
    fn calculate(results: &[TxResult]) -> Self {
        let total_count = results.len();
        let mut success_count = 0;
        let mut failure_count = 0;
        let mut latencies = Vec::new();

        for result in results {
            if result.failed {
                failure_count += 1;
            } else {
                success_count += 1;
                latencies.push(result.latency.as_millis() as f64);
            }
        }

        let (mean_latency_ms, std_dev_ms) = Self::compute_stats(&latencies);

        Self {
            total_count,
            success_count,
            failure_count,
            mean_latency_ms,
            std_dev_ms,
        }
    }

    fn compute_stats(latencies: &[f64]) -> (f64, f64) {
        if latencies.is_empty() {
            return (0.0, 0.0);
        }

        let sum: f64 = latencies.iter().sum();
        let mean = sum / latencies.len() as f64;

        let variance: f64 =
            latencies.iter().map(|l| (l - mean).powi(2)).sum::<f64>() / latencies.len() as f64;
        let std_dev = variance.sqrt();

        (mean, std_dev)
    }

    fn success_rate(&self) -> f64 {
        if self.total_count == 0 {
            0.0
        } else {
            (self.success_count as f64 / self.total_count as f64) * 100.0
        }
    }

    fn failure_rate(&self) -> f64 {
        if self.total_count == 0 {
            0.0
        } else {
            (self.failure_count as f64 / self.total_count as f64) * 100.0
        }
    }

    fn print(&self) {
        println!("\nTransaction Statistics:");
        println!("Total transactions: {}", self.total_count);
        println!(
            "Successful: {} ({:.1}%)",
            self.success_count,
            self.success_rate()
        );
        println!(
            "Failed: {} ({:.1}%)",
            self.failure_count,
            self.failure_rate()
        );

        if self.success_count > 0 {
            println!("\nLatency Statistics (successful transactions only):");
            println!("Average latency: {:.2} ms", self.mean_latency_ms);
            println!("Standard deviation: {:.2} ms", self.std_dev_ms);
        }
    }
}

fn validate_args(args: &Args) -> Result<ValidatedConfig> {
    let submission_delay = parse_duration::parse(&args.submission_delay)
        .map_err(|e| LatencyMonitorError::InvalidSubmissionDelay(e.to_string()))?;

    validate_blob_sizes(args.blob_size_min, args.blob_size)?;

    let private_key = extract_private_key(args)?;
    validate_private_key_hex(&private_key)?;

    let grpc_urls: Vec<String> = args
        .grpc_endpoint
        .iter()
        .map(|endpoint| build_grpc_url(endpoint, args.tls, args.no_tls))
        .collect();

    Ok(ValidatedConfig {
        grpc_urls,
        private_key,
        blob_size_min: args.blob_size_min,
        blob_size_max: args.blob_size,
        namespace: args.namespace.clone(),
        submission_delay,
        disable_metrics: args.disable_metrics,
    })
}

fn validate_blob_sizes(min: usize, max: usize) -> Result<()> {
    if min < 1 {
        return Err(LatencyMonitorError::InvalidMinBlobSize);
    }
    if max < min {
        return Err(LatencyMonitorError::InvalidBlobSizeRange);
    }
    Ok(())
}

fn extract_private_key(args: &Args) -> Result<String> {
    if let Some(ref key) = args.private_key {
        return Ok(key.clone());
    }

    if args.keyring_dir.is_some() {
        eprintln!(
            "Warning: --keyring-dir is not supported in this Rust implementation. \
            Please use --private-key or CELESTIA_PRIVATE_KEY env var."
        );
    }

    Err(LatencyMonitorError::MissingPrivateKey)
}

fn validate_private_key_hex(key: &str) -> Result<()> {
    hex::decode(key).map_err(|_| LatencyMonitorError::InvalidPrivateKeyHex)?;
    Ok(())
}

fn build_grpc_url(endpoint: &str, use_tls: bool, use_no_tls: bool) -> String {
    let stripped = strip_scheme(endpoint);

    if use_tls {
        format!("https://{}", stripped)
    } else if use_no_tls {
        format!("http://{}", stripped)
    } else if endpoint.contains("://") {
        endpoint.to_string()
    } else {
        format!("http://{}", endpoint)
    }
}

fn strip_scheme(endpoint: &str) -> &str {
    const SCHEMES: &[&str] = &["http://", "https://", "ws://", "wss://"];

    for scheme in SCHEMES {
        if let Some(stripped) = endpoint.strip_prefix(scheme) {
            return stripped;
        }
    }
    endpoint
}

fn create_namespace(namespace_str: &str) -> Result<Namespace> {
    let mut ns_bytes = [0u8; 10];
    let bytes = namespace_str.as_bytes();
    let len = bytes.len().min(10);
    ns_bytes[..len].copy_from_slice(&bytes[..len]);

    Namespace::new_v0(&ns_bytes).map_err(|e| LatencyMonitorError::NamespaceError(e.to_string()))
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
        .unwrap_or("")
        .to_string()
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

async fn run_submission_loop(
    client: Arc<GrpcClient>,
    config: Arc<ValidatedConfig>,
    results: Arc<Mutex<Vec<TxResult>>>,
    shutdown: Arc<Notify>,
) {
    let namespace = match create_namespace(&config.namespace) {
        Ok(ns) => ns,
        Err(e) => {
            eprintln!("Failed to create namespace: {}", e);
            return;
        }
    };

    let mut submission_ticker = time::interval(config.submission_delay);
    let mut status_ticker = time::interval(Duration::from_secs(10));
    let mut counter = 0u64;

    loop {
        tokio::select! {
            _ = submission_ticker.tick() => {
                counter += 1;
                spawn_submission_task(
                    client.clone(),
                    namespace,
                    config.blob_size_min,
                    config.blob_size_max,
                    config.disable_metrics,
                    results.clone(),
                );
            }
            _ = status_ticker.tick() => {
                println!("Transactions submitted: {}", counter);
            }
            _ = shutdown.notified() => {
                return;
            }
        }
    }
}

fn spawn_submission_task(
    client: Arc<GrpcClient>,
    namespace: Namespace,
    size_min: usize,
    size_max: usize,
    disable_metrics: bool,
    results: Arc<Mutex<Vec<TxResult>>>,
) {
    tokio::spawn(async move {
        let result = submit_transaction(&client, namespace, size_min, size_max).await;

        if !disable_metrics {
            results.lock().await.push(result);
        }
    });
}

fn write_results_to_csv(results: &[TxResult]) -> Result<()> {
    let file = std::fs::File::create("latency_results.csv")?;
    let mut writer = csv::Writer::from_writer(file);

    write_csv_header(&mut writer)?;

    for result in results {
        write_csv_row(&mut writer, result)?;
    }

    writer.flush()?;
    Ok(())
}

fn write_csv_header<W: std::io::Write>(writer: &mut csv::Writer<W>) -> Result<()> {
    writer.write_record([
        "Submit Time",
        "Commit Time",
        "Latency (ms)",
        "Tx Hash",
        "Height",
        "Code",
        "Failed",
        "Error",
    ])?;
    Ok(())
}

fn write_csv_row<W: std::io::Write>(writer: &mut csv::Writer<W>, result: &TxResult) -> Result<()> {
    let latency_str = if result.failed {
        String::new()
    } else {
        format!("{:.2}", result.latency.as_millis() as f64)
    };

    writer.write_record([
        humantime::format_rfc3339(result.submit_time).to_string(),
        humantime::format_rfc3339(result.commit_time).to_string(),
        latency_str,
        result.tx_hash.clone(),
        result.height.to_string(),
        result.code.to_string(),
        result.failed.to_string(),
        result.error_msg.clone(),
    ])?;

    Ok(())
}

fn output_results(results: &[TxResult]) -> Result<()> {
    write_results_to_csv(results)?;

    let stats = Statistics::calculate(results);
    stats.print();

    println!("\nResults written to latency_results.csv");
    Ok(())
}

fn create_grpc_client(config: &ValidatedConfig) -> Result<GrpcClient> {
    if config.grpc_urls.len() == 1 {
        println!("Connecting to gRPC endpoint: {}", config.grpc_urls[0]);
    } else {
        println!(
            "Connecting to gRPC endpoints (with fallback): {}",
            config.grpc_urls.join(", ")
        );
    }

    let mut builder = GrpcClient::builder();
    for url in &config.grpc_urls {
        builder = builder.url(url);
    }

    let client = builder
        .private_key_hex(&config.private_key)
        .build()
        .map_err(|e| LatencyMonitorError::GrpcClientError(e.to_string()))?;

    if let Some(addr) = client.get_account_address() {
        println!("Using account: {}", addr);
    }

    Ok(client)
}

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

    println!("Submitting transactions...");

    let loop_handle = tokio::spawn(run_submission_loop(
        client.clone(),
        config.clone(),
        results.clone(),
        shutdown.clone(),
    ));

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

fn print_startup_info(config: &ValidatedConfig) {
    println!(
        "Monitoring latency with min blob size: {} bytes, max blob size: {} bytes, \
        submission delay: {:?}, namespace: {}, endpoints: {}",
        config.blob_size_min,
        config.blob_size_max,
        config.submission_delay,
        config.namespace,
        config.grpc_urls.len()
    );
    println!("Press Ctrl+C to stop\n");
}

async fn wait_for_shutdown() -> anyhow::Result<()> {
    signal::ctrl_c().await.context("failed to listen for shutdown signal")
}
