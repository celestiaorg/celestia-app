mod keyring;

use std::sync::atomic::{AtomicI64, AtomicU64, Ordering};
use std::sync::Arc;
use std::time::{Duration, Instant};

use anyhow::Context;
use celestia_fibre::{BlobID, FibreClient, FibreClientConfig};
use celestia_grpc::{GrpcClient, TxConfig};
use clap::Parser;
use k256::ecdsa::SigningKey;
use rand::Rng;
use tokio::sync::mpsc;

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
}

/// Parse a duration string, handling bare "0" which humantime doesn't accept.
fn parse_duration(s: &str) -> Result<Duration, String> {
    let s = s.trim();
    if s == "0" {
        return Ok(Duration::ZERO);
    }
    humantime::parse_duration(s).map_err(|e| format!("invalid duration '{s}': {e}"))
}

/// Per-worker state: each worker has its own fibre client, gRPC client, and key.
struct Worker {
    fibre_client: Arc<FibreClient>,
    grpc_client: Arc<GrpcClient>,
    signer_address: String,
    key_name: String,
}

/// A download request sent from an upload worker to a download worker.
struct DownloadRequest {
    blob_id: BlobID,
    original_data: Vec<u8>,
    fibre_client: Arc<FibreClient>,
    key_name: String,
}

/// Shared stats across all workers.
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

    tracing_subscriber::fmt()
        .with_env_filter(filter)
        .init();

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

    // Create one worker per concurrent slot, each with its own account
    let mut workers = Vec::with_capacity(args.concurrency);
    for i in 0..args.concurrency {
        let key_name = format!("{}-{}", args.key_prefix, i);

        let local_key = kr
            .local_key(&key_name)
            .with_context(|| format!("failed to read key '{}' from keyring", key_name))?;

        let private_key_hex = local_key.private_key_hex();
        let signing_key = SigningKey::from_bytes(local_key.private_key.as_slice().into())
            .context("invalid secp256k1 private key")?;

        let grpc_client = GrpcClient::builder()
            .url(&grpc_url)
            .private_key_hex(&private_key_hex)
            .build()
            .with_context(|| format!("failed to build gRPC client for worker {}", i))?;

        let mut config = FibreClientConfig::default();
        config.chain_id = args.chain_id.clone();

        let fibre_client =
            FibreClient::from_grpc_client(grpc_client.clone(), signing_key, config)
                .with_context(|| format!("failed to create fibre client for worker {}", i))?;

        let signer_address = grpc_client
            .get_account_address()
            .context("no signer address on grpc client")?
            .to_string();

        println!("Worker {} initialized with key {}", i, key_name);

        workers.push(Worker {
            fibre_client: Arc::new(fibre_client),
            grpc_client: Arc::new(grpc_client),
            signer_address,
            key_name,
        });
    }

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
        "\nStarting fibre blob spam with {} workers...",
        args.concurrency
    );

    let blob_size = args.blob_size;
    let interval = args.interval;
    let download = args.download;

    // Download channel: upload workers send requests, download workers process them
    // Use concurrency as buffer size so uploads aren't blocked waiting for downloads
    let (dl_tx, dl_rx) = if download {
        let (tx, rx) = mpsc::channel::<DownloadRequest>(args.concurrency * 4);
        (Some(tx), Some(rx))
    } else {
        (None, None)
    };

    let mut tasks = tokio::task::JoinSet::new();

    // Spawn download workers (same count as upload workers)
    if let Some(dl_rx) = dl_rx {
        let dl_rx = Arc::new(tokio::sync::Mutex::new(dl_rx));
        for _ in 0..args.concurrency {
            let dl_rx = Arc::clone(&dl_rx);
            let stats = Arc::clone(&stats);
            let cancel = cancel.clone();

            tasks.spawn(async move {
                download_worker_loop(dl_rx, &stats, cancel).await;
            });
        }
    }

    // Spawn upload workers
    for w in workers {
        let stats = Arc::clone(&stats);
        let cancel = cancel.clone();
        let dl_tx = dl_tx.clone();

        tasks.spawn(async move {
            upload_worker_loop(w, blob_size, interval, dl_tx, stats, cancel).await;
        });
    }

    // Drop our copy of the sender so download workers can detect when uploads are done
    drop(dl_tx);

    // Wait for all workers to finish
    while tasks.join_next().await.is_some() {}

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
    println!(
        "  Total sent: {}",
        stats.total_sent.load(Ordering::Relaxed)
    );
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

async fn upload_worker_loop(
    w: Worker,
    blob_size: usize,
    interval: Duration,
    dl_tx: Option<mpsc::Sender<DownloadRequest>>,
    stats: Arc<Stats>,
    cancel: tokio_util::sync::CancellationToken,
) {
    loop {
        if cancel.is_cancelled() {
            return;
        }

        upload_blob(&w, blob_size, dl_tx.as_ref(), &stats, &cancel).await;

        if interval > Duration::ZERO {
            tokio::select! {
                _ = tokio::time::sleep(interval) => {}
                _ = cancel.cancelled() => return,
            }
        }
    }
}

async fn upload_blob(
    w: &Worker,
    blob_size: usize,
    dl_tx: Option<&mpsc::Sender<DownloadRequest>>,
    stats: &Stats,
    cancel: &tokio_util::sync::CancellationToken,
) {
    let (ns, data) = {
        let mut rng = rand::thread_rng();
        let mut ns = vec![0u8; 29];
        rng.fill(&mut ns[19..29]);
        let mut data = vec![0u8; blob_size];
        rng.fill(&mut data[..]);
        (ns, data)
    };

    let t = Instant::now();
    let result = w.fibre_client.put(&ns, &data, &w.signer_address).await;

    stats.total_sent.fetch_add(1, Ordering::Relaxed);
    match result {
        Ok(prepared) => {
            if cancel.is_cancelled() {
                return;
            }
            let blob_id = prepared.blob_id;
            match w
                .grpc_client
                .broadcast_message(prepared.msg, TxConfig::default())
                .await
            {
                Ok(submitted_tx) => match submitted_tx.confirm().await {
                    Ok(tx_info) => {
                        let lat = t.elapsed();
                        stats.successes.fetch_add(1, Ordering::Relaxed);
                        stats
                            .total_lat_ns
                            .fetch_add(lat.as_nanos() as i64, Ordering::Relaxed);
                        println!(
                            "[{}] upload: height={} tx={} latency={:?}",
                            w.key_name, tx_info.height, tx_info.hash, lat
                        );
                        // Send download request to download workers (non-blocking)
                        if let Some(dl_tx) = dl_tx {
                            let _ = dl_tx.try_send(DownloadRequest {
                                blob_id,
                                original_data: data,
                                fibre_client: Arc::clone(&w.fibre_client),
                                key_name: w.key_name.clone(),
                            });
                        }
                    }
                    Err(e) => {
                        if cancel.is_cancelled() {
                            return;
                        }
                        let lat = t.elapsed();
                        stats.failures.fetch_add(1, Ordering::Relaxed);
                        println!(
                            "[{}] confirm error: {} (latency={:?})",
                            w.key_name, e, lat
                        );
                    }
                },
                Err(e) => {
                    if cancel.is_cancelled() {
                        return;
                    }
                    let lat = t.elapsed();
                    stats.failures.fetch_add(1, Ordering::Relaxed);
                    println!(
                        "[{}] broadcast error: {} (latency={:?})",
                        w.key_name, e, lat
                    );
                }
            }
        }
        Err(e) => {
            if cancel.is_cancelled() {
                return;
            }
            let lat = t.elapsed();
            stats.failures.fetch_add(1, Ordering::Relaxed);
            println!("[{}] upload error: {} (latency={:?})", w.key_name, e, lat);
        }
    }
}

async fn download_worker_loop(
    dl_rx: Arc<tokio::sync::Mutex<mpsc::Receiver<DownloadRequest>>>,
    stats: &Stats,
    cancel: tokio_util::sync::CancellationToken,
) {
    loop {
        let req = {
            let mut rx = dl_rx.lock().await;
            tokio::select! {
                req = rx.recv() => req,
                _ = cancel.cancelled() => {
                    // Drain remaining requests before exiting
                    while let Ok(req) = rx.try_recv() {
                        download_blob(&req, stats).await;
                    }
                    return;
                }
            }
        };

        match req {
            Some(req) => download_blob(&req, stats).await,
            None => return, // Channel closed, all upload workers done
        }
    }
}

async fn download_blob(req: &DownloadRequest, stats: &Stats) {
    let t = Instant::now();
    match req.fibre_client.download(&req.blob_id).await {
        Ok(blob) => {
            let lat = t.elapsed();
            let verified = blob.data() == Some(req.original_data.as_slice());
            stats.dl_successes.fetch_add(1, Ordering::Relaxed);
            stats
                .dl_total_lat_ns
                .fetch_add(lat.as_nanos() as i64, Ordering::Relaxed);
            if verified {
                stats.dl_verified.fetch_add(1, Ordering::Relaxed);
            }
            println!(
                "[{}] download: blob_id={} latency={:?} verified={}",
                req.key_name, req.blob_id, lat, verified
            );
        }
        Err(e) => {
            stats.dl_failures.fetch_add(1, Ordering::Relaxed);
            println!(
                "[{}] download error: blob_id={} {} (latency={:?})",
                req.key_name,
                req.blob_id,
                e,
                t.elapsed()
            );
        }
    }
}
