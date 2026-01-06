use std::path::PathBuf;
use std::time::Duration;

use celestia_types::nmt::Namespace;
use clap::Parser;
use humantime::parse_duration;
use thiserror::Error;

use crate::keyring::{Backend, FileKeyring, KeyringError};

/// Default keyring directory (matches Go latency-monitor)
const DEFAULT_KEYRING_DIR: &str = "~/.celestia-app";

#[derive(Parser, Debug)]
#[command(name = "lumina-latency-monitor")]
#[command(about = "Monitor and measure transaction latency in Celestia networks")]
pub struct Args {
    /// gRPC endpoint URL
    #[arg(short = 'e', long, default_value = "localhost:9090")]
    pub grpc_endpoint: String,

    /// Directory containing the keyring (default: ~/.celestia-app)
    #[arg(short = 'k', long)]
    pub keyring_dir: Option<PathBuf>,

    /// Account name to use from keyring (defaults to first account)
    #[arg(short = 'a', long)]
    pub account: Option<String>,

    /// Private key hex (alternative to keyring)
    #[arg(short = 'p', long, env = "CELESTIA_PRIVATE_KEY")]
    pub private_key: Option<String>,

    /// Maximum blob size in bytes
    #[arg(short = 'b', long, default_value_t = 1024)]
    pub blob_size: usize,

    /// Minimum blob size in bytes
    #[arg(short = 'z', long, default_value_t = 1)]
    pub blob_size_min: usize,

    /// Namespace for blob submission
    #[arg(short = 'n', long, default_value = "test")]
    pub namespace: String,

    /// Disable metrics collection
    #[arg(short = 'm', long)]
    pub disable_metrics: bool,

    /// Delay between transaction submissions
    #[arg(short = 'd', long, default_value = "4000ms")]
    pub submission_delay: String,

    /// Port for Prometheus metrics HTTP server (0 to disable)
    #[arg(long, default_value_t = 9464)]
    pub metrics_port: u16,
}

#[derive(Error, Debug)]
pub enum LatencyMonitorError {
    #[error("minimum blob size must be at least 1 byte")]
    InvalidMinBlobSize,

    #[error("maximum blob size must be greater than or equal to minimum blob size")]
    InvalidBlobSizeRange,

    #[error("private key is required (use --private-key, CELESTIA_PRIVATE_KEY, or keyring)")]
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

    #[error("keyring error: {0}")]
    KeyringError(#[from] KeyringError),

    #[error("CSV write error: {0}")]
    CsvError(#[from] csv::Error),

    #[error("IO error: {0}")]
    IoError(#[from] std::io::Error),
}

pub type Result<T> = std::result::Result<T, LatencyMonitorError>;

pub struct ValidatedConfig {
    pub grpc_url: String,
    pub private_key: String,
    pub account_name: String,
    pub account_address: String,
    pub blob_size_min: usize,
    pub blob_size_max: usize,
    pub namespace: Namespace,
    pub submission_delay: Duration,
    pub disable_metrics: bool,
    pub metrics_port: u16,
}

pub fn validate_args(args: &Args) -> Result<ValidatedConfig> {
    let submission_delay = parse_duration(&args.submission_delay)
        .map_err(|e| LatencyMonitorError::InvalidSubmissionDelay(e.to_string()))?;

    validate_blob_sizes(args.blob_size_min, args.blob_size)?;

    let (private_key, account_name, account_address) = extract_private_key(args)?;
    validate_private_key_hex(&private_key)?;

    let grpc_url = build_grpc_url(&args.grpc_endpoint);
    let namespace = create_namespace(&args.namespace)?;

    Ok(ValidatedConfig {
        grpc_url,
        private_key,
        account_name,
        account_address,
        blob_size_min: args.blob_size_min,
        blob_size_max: args.blob_size,
        namespace,
        submission_delay,
        disable_metrics: args.disable_metrics,
        metrics_port: args.metrics_port,
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

/// Extract private key from either --private-key flag or keyring
/// Returns (private_key_hex, account_name, account_address)
fn extract_private_key(args: &Args) -> Result<(String, String, String)> {
    // Option 1: Explicit private key via flag or env var
    if let Some(ref key) = args.private_key {
        return Ok((key.clone(), "direct".to_string(), "unknown".to_string()));
    }

    // Option 2: From keyring
    let keyring_dir = args
        .keyring_dir
        .as_ref()
        .map(|p| p.to_string_lossy().to_string())
        .unwrap_or_else(|| DEFAULT_KEYRING_DIR.to_string());

    let keyring = FileKeyring::open(&keyring_dir, Backend::Test)?;

    // Get account name (specified or first available)
    let account_name = match &args.account {
        Some(name) => name.clone(),
        None => keyring.first_key()?,
    };

    let local_key = keyring.local_key(&account_name)?;

    Ok((
        local_key.private_key_hex(),
        local_key.record.name,
        local_key.record.address,
    ))
}

fn validate_private_key_hex(key: &str) -> Result<()> {
    hex::decode(key).map_err(|_| LatencyMonitorError::InvalidPrivateKeyHex)?;
    Ok(())
}

fn build_grpc_url(endpoint: &str) -> String {
    let stripped = strip_scheme(endpoint);
    format!("http://{}", stripped)
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
