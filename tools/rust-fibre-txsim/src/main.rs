mod keyring;

use std::future::Future;
use std::pin::Pin;
use std::sync::atomic::{AtomicI64, AtomicU64, Ordering};
use std::sync::Arc;
use std::time::{Duration, Instant};

use anyhow::Context;
use celestia_fibre::{Blob, BlobConfig, BlobID, DownloadOptions, FibreClient, FibreClientConfig};
use celestia_grpc::{
    AccountSigner, GrpcClient, MultiAccountTxService, MultiAccountTxServiceConfig, TxConfig, TxInfo,
};
use celestia_proto::celestia::fibre::v1::MsgPayForFibre;
use celestia_types::state::AccAddress;
use clap::Parser;
use k256::ecdsa::SigningKey;
use rand::Rng;
use tokio::task::JoinSet;

use keyring::{Backend, FileKeyring};

/// Fibre blob submission stress tool.
///
/// Submits random blobs via the Fibre protocol at a configurable rate,
/// tracking latency, successes, and failures. Each concurrent worker
/// uses its own account (key-prefix-0, key-prefix-1, ...) to avoid
/// sequence number conflicts.
#[derive(Parser, Debug)]
#[command(name = "fibre-txsim")]
struct Args {
    /// Chain ID (required)
    #[arg(long)]
    chain_id: String,

    /// gRPC endpoint
    #[arg(long, default_value = "localhost:9091")]
    grpc_endpoint: String,

    /// Keyring directory
    #[arg(long, default_value = ".celestia-app")]
    keyring_dir: String,

    /// Key name prefix in keyring (keys are named <prefix>-0, <prefix>-1, ...)
    #[arg(long, default_value = "fibre")]
    key_prefix: String,

    /// Size of each blob in bytes
    #[arg(long, default_value_t = 1_000_000)]
    blob_size: usize,

    /// Number of concurrent blob submissions (each gets its own account)
    #[arg(long, default_value_t = 1)]
    concurrency: usize,

    /// Delay between blob submissions per worker (0 = no delay).
    /// Accepts Go-style durations: 0, 0s, 1s, 500ms, 5m, etc.
    #[arg(long, default_value = "0", value_parser = parse_duration)]
    interval: Duration,

    /// How long to run (0 = until killed).
    /// Accepts Go-style durations: 0, 0s, 1s, 5m, 1h, etc.
    #[arg(long, default_value = "0", value_parser = parse_duration)]
    duration: Duration,

    /// Enable download verification after each successful upload
    #[arg(long, default_value_t = false)]
    download: bool,

    /// Skip PFF transaction — only upload shards to validators without on-chain confirmation (no-op, kept for CLI compatibility)
    #[arg(long, default_value_t = false)]
    upload_only: bool,

    /// OpenTelemetry collector endpoint (no-op, kept for CLI compatibility)
    #[arg(long, default_value = None)]
    otel_endpoint: Option<String>,

    /// Pyroscope endpoint for continuous profiling (no-op, kept for CLI compatibility)
    #[arg(long, default_value = None)]
    pyroscope_endpoint: Option<String>,
}

/// Parse a duration string, handling bare "0" which humantime doesn't accept.
fn parse_duration(s: &str) -> Result<Duration, String> {
    let s = s.trim();
    if s == "0" {
        return Ok(Duration::ZERO);
    }
    humantime::parse_duration(s).map_err(|e| format!("invalid duration '{s}': {e}"))
}

/// Per-account state shared across pipeline stages.
struct AccountInfo {
    fibre_client: Arc<FibreClient>,
    signing_key: SigningKey,
    signer_address: AccAddress,
    signer_address_str: String,
    key_name: String,
}

/// Output of the upload stage, fed into the submit stage.
struct UploadPayload {
    msg: MsgPayForFibre,
    start_time: Instant,
    blob_id: Option<BlobID>,
    original_data: Vec<u8>,
}

/// Output of the submit stage, fed into the confirm stage.
struct SubmitOutput {
    key_name: String,
    start_time: Instant,
    blob_id: Option<BlobID>,
    original_data: Vec<u8>,
    fibre_client: Arc<FibreClient>,
    confirm: Pin<Box<dyn Future<Output = anyhow::Result<TxInfo>> + Send>>,
}

/// Output of the confirm stage.
struct ConfirmOutput {
    key_name: String,
    start_time: Instant,
    blob_id: Option<BlobID>,
    original_data: Vec<u8>,
    fibre_client: Arc<FibreClient>,
    result: anyhow::Result<TxInfo>,
}

/// Output of the download stage.
struct DownloadOutput {
    key_name: String,
    blob_id: BlobID,
    latency: Duration,
    success: bool,
    verified: bool,
}

/// Shared stats across all pipeline stages.
struct Stats {
    total_sent: AtomicU64,
    successes: AtomicU64,
    failures: AtomicU64,
    total_lat_ns: AtomicI64,
    dl_successes: AtomicU64,
    dl_failures: AtomicU64,
    dl_total_lat_ns: AtomicI64,
    dl_verified: AtomicU64,
}

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    let args = Args::parse();

    let filter = tracing_subscriber::EnvFilter::builder()
        .with_default_directive(tracing_subscriber::filter::LevelFilter::INFO.into())
        .from_env_lossy();

    tracing_subscriber::fmt().with_env_filter(filter).init();

    println!(
        "celestia-fibre version: {} (commit: {})",
        env!("CELESTIA_FIBRE_VERSION"),
        env!("CELESTIA_FIBRE_COMMIT"),
    );

    let kr =
        FileKeyring::open(&args.keyring_dir, Backend::Test).context("failed to open keyring")?;

    // Build gRPC endpoint URL
    let grpc_url = if args.grpc_endpoint.starts_with("http://")
        || args.grpc_endpoint.starts_with("https://")
    {
        args.grpc_endpoint.clone()
    } else {
        format!("http://{}", args.grpc_endpoint)
    };

    // Create a single shared FibreClient. All workers reuse the same
    // connections and validator set cache, avoiding connection exhaustion
    // under high concurrency.
    let mut config = FibreClientConfig::default();
    config.chain_id = args.chain_id.clone();
    let shared_fibre_client = Arc::new(
        FibreClient::from_endpoint(&grpc_url, config)
            .context("failed to create shared fibre client")?,
    );

    // Create signers and account info for each concurrent slot
    let mut signers = Vec::with_capacity(args.concurrency);
    let mut accounts: Vec<Arc<AccountInfo>> = Vec::with_capacity(args.concurrency);
    for i in 0..args.concurrency {
        let key_name = format!("{}-{}", args.key_prefix, i);

        let local_key = kr
            .local_key(&key_name)
            .with_context(|| format!("failed to read key '{}' from keyring", key_name))?;

        let private_key_hex = local_key.private_key_hex();
        let signing_key = SigningKey::from_bytes(local_key.private_key.as_slice().into())
            .context("invalid secp256k1 private key")?;

        let signer = AccountSigner::from_private_key_hex(&private_key_hex)
            .with_context(|| format!("failed to create signer for worker {}", i))?;
        let address = signer.address();

        println!(
            "Worker {} initialized with key {} ({})",
            i, key_name, address
        );

        let address_str = address.to_string();
        signers.push(signer);
        accounts.push(Arc::new(AccountInfo {
            fibre_client: Arc::clone(&shared_fibre_client),
            signing_key,
            signer_address: address,
            signer_address_str: address_str,
            key_name,
        }));
    }

    // Create a single shared GrpcClient and MultiAccountTxService.
    // One slot per account with independent sequence tracking;
    // all accounts share the same gRPC connection pool.
    let client = GrpcClient::builder()
        .url(&grpc_url)
        .build()
        .context("failed to build gRPC client")?;
    let nodes = vec![(Arc::from("node-0"), client)];
    let tx_service = MultiAccountTxService::new(MultiAccountTxServiceConfig::new(nodes, signers))
        .await
        .context("failed to create tx service")?;

    let stats = Arc::new(Stats {
        total_sent: AtomicU64::new(0),
        successes: AtomicU64::new(0),
        failures: AtomicU64::new(0),
        total_lat_ns: AtomicI64::new(0),
        dl_successes: AtomicU64::new(0),
        dl_failures: AtomicU64::new(0),
        dl_total_lat_ns: AtomicI64::new(0),
        dl_verified: AtomicU64::new(0),
    });

    let start_time = Instant::now();

    // Shutdown signal
    let cancel = tokio_util::sync::CancellationToken::new();

    // Handle Ctrl+C
    let cancel_signal = cancel.clone();
    tokio::spawn(async move {
        if tokio::signal::ctrl_c().await.is_ok() {
            println!("\nReceived interrupt, shutting down...");
            cancel_signal.cancel();
        }
    });

    // Apply duration limit if set
    if args.duration > Duration::ZERO {
        let cancel_timer = cancel.clone();
        let dur = args.duration;
        tokio::spawn(async move {
            tokio::time::sleep(dur).await;
            cancel_timer.cancel();
        });
    }

    println!(
        "\nStarting fibre blob spam with {} workers (pipeline mode)...",
        args.concurrency
    );

    let blob_size = args.blob_size;
    let interval = args.interval;
    let download = args.download;

    let mut upload_tasks: JoinSet<(Arc<AccountInfo>, anyhow::Result<UploadPayload>)> =
        JoinSet::new();
    let mut submit_tasks: JoinSet<anyhow::Result<SubmitOutput>> = JoinSet::new();
    let mut confirm_tasks: JoinSet<ConfirmOutput> = JoinSet::new();
    let mut download_tasks: JoinSet<DownloadOutput> = JoinSet::new();

    // Seed one upload per account (no delay for initial)
    for account in &accounts {
        upload_tasks.spawn(do_upload(
            account.clone(),
            blob_size,
            download,
            Duration::ZERO,
        ));
    }

    loop {
        tokio::select! {
            Some(result) = upload_tasks.join_next(), if !upload_tasks.is_empty() => {
                let (account, result) = result.expect("upload task panicked");
                stats.total_sent.fetch_add(1, Ordering::Relaxed);
                match result {
                    Ok(payload) => {
                        submit_tasks.spawn(do_submit(
                            tx_service.clone(),
                            account.clone(),
                            payload,
                        ));
                    }
                    Err(e) => {
                        stats.failures.fetch_add(1, Ordering::Relaxed);
                        eprintln!("[{}] upload error: {e}", account.key_name);
                    }
                }
                // Always respawn upload for this account (delay is inside do_upload)
                upload_tasks.spawn(do_upload(account, blob_size, download, interval));
            }
            Some(result) = submit_tasks.join_next(), if !submit_tasks.is_empty() => {
                match result.expect("submit task panicked") {
                    Ok(submit_out) => {
                        confirm_tasks.spawn(do_confirm(submit_out));
                    }
                    Err(e) => {
                        stats.failures.fetch_add(1, Ordering::Relaxed);
                        eprintln!("submit error: {e}");
                    }
                }
            }
            Some(result) = confirm_tasks.join_next(), if !confirm_tasks.is_empty() => {
                let out = result.expect("confirm task panicked");
                match out.result {
                    Ok(tx_info) => {
                        let lat = out.start_time.elapsed();
                        stats.successes.fetch_add(1, Ordering::Relaxed);
                        stats
                            .total_lat_ns
                            .fetch_add(lat.as_nanos() as i64, Ordering::Relaxed);
                        println!(
                            "[{}] confirmed: height={} tx={} latency={:?}",
                            out.key_name, tx_info.height, tx_info.hash, lat
                        );
                        if download {
                            if let Some(blob_id) = out.blob_id {
                                download_tasks.spawn(do_download(
                                    out.fibre_client,
                                    blob_id,
                                    out.original_data,
                                    out.key_name,
                                    Duration::from_secs(10),
                                ));
                            }
                        }
                    }
                    Err(e) => {
                        stats.failures.fetch_add(1, Ordering::Relaxed);
                        eprintln!("[{}] confirm error: {e}", out.key_name);
                    }
                }
            }
            Some(result) = download_tasks.join_next(), if !download_tasks.is_empty() => {
                let out = result.expect("download task panicked");
                if out.success {
                    stats.dl_successes.fetch_add(1, Ordering::Relaxed);
                    stats
                        .dl_total_lat_ns
                        .fetch_add(out.latency.as_nanos() as i64, Ordering::Relaxed);
                    if out.verified {
                        stats.dl_verified.fetch_add(1, Ordering::Relaxed);
                    }
                } else {
                    stats.dl_failures.fetch_add(1, Ordering::Relaxed);
                }
            }
            _ = cancel.cancelled() => break,
        }
    }

    let elapsed = start_time.elapsed();
    let s = stats.successes.load(Ordering::Relaxed);
    let f = stats.failures.load(Ordering::Relaxed);
    let avg_lat = if s > 0 {
        Duration::from_nanos((stats.total_lat_ns.load(Ordering::Relaxed) as u64) / s)
    } else {
        Duration::ZERO
    };

    println!("\n--- Summary ---");
    println!("Duration:   {:.0?}", elapsed);
    println!();
    println!("Uploads:");
    println!("  Total sent: {}", stats.total_sent.load(Ordering::Relaxed));
    println!("  Successes:  {}", s);
    println!("  Failures:   {}", f);
    println!("  Avg latency (success): {:?}", avg_lat);

    if download {
        let ds = stats.dl_successes.load(Ordering::Relaxed);
        let df = stats.dl_failures.load(Ordering::Relaxed);
        let dv = stats.dl_verified.load(Ordering::Relaxed);
        let avg_dl_lat = if ds > 0 {
            Duration::from_nanos((stats.dl_total_lat_ns.load(Ordering::Relaxed) as u64) / ds)
        } else {
            Duration::ZERO
        };

        println!();
        println!("Downloads:");
        println!("  Successes:  {}", ds);
        println!("  Failures:   {}", df);
        println!("  Verified:   {}", dv);
        println!("  Avg latency (success): {:?}", avg_dl_lat);
    }

    Ok(())
}

/// Upload stage: generate random blob data, call `put()` to upload to fibre nodes.
/// Sleeps for `delay` first (0 for initial seed, `interval` for subsequent uploads).
/// Always returns the account Arc so the main loop can respawn.
async fn do_upload(
    account: Arc<AccountInfo>,
    blob_size: usize,
    download: bool,
    delay: Duration,
) -> (Arc<AccountInfo>, anyhow::Result<UploadPayload>) {
    if delay > Duration::ZERO {
        tokio::time::sleep(delay).await;
    }

    let result = async {
        let (ns, data) = {
            let mut rng = rand::thread_rng();
            let mut ns = vec![0u8; 29];
            rng.fill(&mut ns[19..29]);
            let mut data = vec![0u8; blob_size];
            rng.fill(&mut data[..]);
            (ns, data)
        };

        let blob_id = if download {
            BlobConfig::for_version(0)
                .ok()
                .and_then(|cfg| Blob::new(&data, cfg).ok())
                .map(|blob| blob.id().clone())
        } else {
            None
        };

        let start_time = Instant::now();
        let msg = account
            .fibre_client
            .upload_and_prepare(
                &account.signing_key,
                &ns,
                &data,
                &account.signer_address_str,
            )
            .await?;

        Ok(UploadPayload {
            msg,
            start_time,
            blob_id,
            original_data: data,
        })
    }
    .await;

    (account, result)
}

/// Submit stage: send the tx message via the MultiAccountTxService.
/// Captures the confirm future in a boxed future for the next stage.
async fn do_submit(
    tx_service: MultiAccountTxService,
    account: Arc<AccountInfo>,
    payload: UploadPayload,
) -> anyhow::Result<SubmitOutput> {
    let handle = tx_service
        .submit_message(&account.signer_address, payload.msg, TxConfig::default())
        .await
        .with_context(|| format!("[{}] submit failed", account.key_name))?;

    let confirm: Pin<Box<dyn Future<Output = anyhow::Result<TxInfo>> + Send>> =
        Box::pin(async move { handle.confirm().await.map_err(|e| anyhow::anyhow!("{e}")) });

    Ok(SubmitOutput {
        key_name: account.key_name.clone(),
        start_time: payload.start_time,
        blob_id: payload.blob_id,
        original_data: payload.original_data,
        fibre_client: Arc::clone(&account.fibre_client),
        confirm,
    })
}

/// Confirm stage: await the boxed confirm future from the submit stage.
async fn do_confirm(submit_out: SubmitOutput) -> ConfirmOutput {
    let result = submit_out.confirm.await;
    ConfirmOutput {
        key_name: submit_out.key_name,
        start_time: submit_out.start_time,
        blob_id: submit_out.blob_id,
        original_data: submit_out.original_data,
        fibre_client: submit_out.fibre_client,
        result,
    }
}

/// Download stage: wait for `delay`, then download the blob and verify its contents.
async fn do_download(
    fibre_client: Arc<FibreClient>,
    blob_id: BlobID,
    original_data: Vec<u8>,
    key_name: String,
    delay: Duration,
) -> DownloadOutput {
    if delay > Duration::ZERO {
        tokio::time::sleep(delay).await;
    }
    let t = Instant::now();
    match fibre_client
        .download(&blob_id, DownloadOptions::default())
        .await
    {
        Ok(blob) => {
            let latency = t.elapsed();
            let verified = blob.data() == Some(original_data.as_slice());
            println!(
                "[{}] download: blob_id={} latency={:?} verified={}",
                key_name, blob_id, latency, verified
            );
            DownloadOutput {
                key_name,
                blob_id,
                latency,
                success: true,
                verified,
            }
        }
        Err(e) => {
            let latency = t.elapsed();
            println!(
                "[{}] download error: blob_id={} {} (latency={:?})",
                key_name, blob_id, e, latency
            );
            DownloadOutput {
                key_name,
                blob_id,
                latency,
                success: false,
                verified: false,
            }
        }
    }
}
