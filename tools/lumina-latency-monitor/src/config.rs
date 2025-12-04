use std::path::PathBuf;
use std::time::Duration;

use celestia_types::nmt::Namespace;
use clap::Parser;
use thiserror::Error;

#[derive(Parser, Debug)]
#[command(name = "lumina-latency-monitor")]
#[command(about = "Monitor and measure transaction latency in Celestia networks")]
pub struct Args {
    /// gRPC endpoint URL
    #[arg(short = 'e', long, default_value = "localhost:9090")]
    pub grpc_endpoint: String,

    #[arg(short = 'k', long)]
    pub keyring_dir: Option<PathBuf>,

    #[arg(short = 'a', long)]
    pub account: Option<String>,

    #[arg(short = 'p', long, env = "CELESTIA_PRIVATE_KEY")]
    pub private_key: Option<String>,

    #[arg(short = 'b', long, default_value_t = 1024)]
    pub blob_size: usize,

    #[arg(short = 'z', long, default_value_t = 1)]
    pub blob_size_min: usize,

    #[arg(short = 'n', long, default_value = "test")]
    pub namespace: String,

    #[arg(short = 'm', long)]
    pub disable_metrics: bool,

    #[arg(short = 'd', long, default_value = "4000ms")]
    pub submission_delay: String,

    #[arg(long, conflicts_with = "no_tls")]
    pub tls: bool,

    #[arg(long, alias = "insecure", conflicts_with = "tls")]
    pub no_tls: bool,
}

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

    #[error("CSV write error: {0}")]
    CsvError(#[from] csv::Error),

    #[error("IO error: {0}")]
    IoError(#[from] std::io::Error),
}

pub type Result<T> = std::result::Result<T, LatencyMonitorError>;

pub struct ValidatedConfig {
    pub grpc_url: String,
    pub private_key: String,
    pub blob_size_min: usize,
    pub blob_size_max: usize,
    pub namespace: Namespace,
    pub submission_delay: Duration,
    pub disable_metrics: bool,
}

pub fn validate_args(args: &Args) -> Result<ValidatedConfig> {
    let submission_delay = parse_duration::parse(&args.submission_delay)
        .map_err(|e| LatencyMonitorError::InvalidSubmissionDelay(e.to_string()))?;

    validate_blob_sizes(args.blob_size_min, args.blob_size)?;

    let private_key = extract_private_key(args)?;
    validate_private_key_hex(&private_key)?;

    let grpc_url = build_grpc_url(&args.grpc_endpoint, args.tls, args.no_tls);
    let namespace = create_namespace(&args.namespace)?;

    Ok(ValidatedConfig {
        grpc_url,
        private_key,
        blob_size_min: args.blob_size_min,
        blob_size_max: args.blob_size,
        namespace,
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
