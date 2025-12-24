use std::env;
use std::path::{Path, PathBuf};
use std::process::{Command, ExitCode, Stdio};

const DEFAULT_LINUX_TARGET: &str = "x86_64-unknown-linux-gnu";
const DEFAULT_LINUX_MUSL_TARGET: &str = "x86_64-unknown-linux-musl";
const BIN_NAME: &str = "lumina-latency-monitor";
const PACKAGE_NAME: &str = "lumina-latency-monitor";

fn main() -> ExitCode {
    let args: Vec<String> = env::args().skip(1).collect();

    if args.is_empty() {
        print_usage();
        return ExitCode::FAILURE;
    }

    let result = match args[0].as_str() {
        "setup" => cmd_setup(&args[1..]),
        "build-linux" => cmd_build_linux(&args[1..]),
        "build-linux-gnu" => cmd_build_with_target(DEFAULT_LINUX_TARGET, &args[1..]),
        "build-linux-musl" => cmd_build_with_target(DEFAULT_LINUX_MUSL_TARGET, &args[1..]),
        "help" | "--help" | "-h" => {
            print_usage();
            Ok(())
        }
        cmd => Err(format!("Unknown command: {cmd}").into()),
    };

    match result {
        Ok(()) => ExitCode::SUCCESS,
        Err(e) => {
            eprintln!("Error: {e}");
            ExitCode::FAILURE
        }
    }
}

fn print_usage() {
    println!("Usage: cargo xtask <command> [options]");
    println!();
    println!("Commands:");
    println!("  setup                  Install zig and cargo-zigbuild; add Rust target");
    println!("  build-linux            Build {BIN_NAME} for Linux (default gnu)");
    println!("  build-linux-gnu        Build for {DEFAULT_LINUX_TARGET}");
    println!("  build-linux-musl       Build for {DEFAULT_LINUX_MUSL_TARGET}");
    println!("  help                   Show this help message");
    println!();
    println!("Options:");
    println!("  --target <triple>      Override the target for build-linux / setup");
    println!();
    println!("Examples:");
    println!("  cargo xtask setup");
    println!("  cargo xtask setup --target {DEFAULT_LINUX_MUSL_TARGET}");
    println!("  cargo xtask build-linux");
    println!("  cargo xtask build-linux --target {DEFAULT_LINUX_MUSL_TARGET}");
}

fn cmd_setup(args: &[String]) -> Result<(), Box<dyn std::error::Error>> {
    let target =
        parse_flag_value(args, "--target").unwrap_or_else(|| DEFAULT_LINUX_TARGET.to_string());

    println!("==> Setting up cross-compilation environment...");

    if command_exists("zig") {
        let version = output_checked("zig", &["version"])?;
        println!("==> zig is already installed: {}", version.trim());
    } else {
        install_zig()?;
    }

    if command_exists("cargo-zigbuild") {
        println!("==> cargo-zigbuild is already installed");
    } else {
        println!("==> Installing cargo-zigbuild...");
        run_checked("cargo", &["install", "cargo-zigbuild"], None)?;
    }

    println!("==> Adding Rust target {}...", target);
    run_checked("rustup", &["target", "add", &target], None)?;

    println!();
    println!("==> Setup complete! You can now run:");
    println!("    cargo xtask build-linux");
    println!("    cargo xtask build-linux --target {DEFAULT_LINUX_MUSL_TARGET}");

    Ok(())
}

fn cmd_build_linux(args: &[String]) -> Result<(), Box<dyn std::error::Error>> {
    let target =
        parse_flag_value(args, "--target").unwrap_or_else(|| DEFAULT_LINUX_TARGET.to_string());
    cmd_build_with_target(&target, args)
}

fn cmd_build_with_target(target: &str, _args: &[String]) -> Result<(), Box<dyn std::error::Error>> {
    if !command_exists("cargo-zigbuild") {
        return Err("cargo-zigbuild not found. Run 'cargo xtask setup' first.".into());
    }

    let workspace_root = workspace_root_via_metadata()?;

    println!("==> Building {BIN_NAME} for {target}...");

    run_checked(
        "cargo",
        &[
            "zigbuild",
            "-p",
            PACKAGE_NAME,
            "--release",
            "--target",
            target,
        ],
        Some(&workspace_root),
    )?;

    let binary_path = workspace_root
        .join("target")
        .join(target)
        .join("release")
        .join(BIN_NAME);

    println!("==> Built: {}", binary_path.display());
    Ok(())
}

fn install_zig() -> Result<(), Box<dyn std::error::Error>> {
    let os = env::consts::OS;

    match os {
        "macos" => {
            if command_exists("brew") {
                println!("==> Installing zig via Homebrew...");
                run_checked("brew", &["install", "zig"], None)
            } else {
                Err("Homebrew not found. Install zig manually (or via zigup): https://ziglang.org/download/".into())
            }
        }
        "linux" => {
            if command_exists("apt-get") {
                println!("==> Installing zig via apt (best-effort)...");
                run_checked("sudo", &["apt-get", "update"], None)?;
                run_checked("sudo", &["apt-get", "install", "-y", "zig"], None)
            } else if command_exists("dnf") {
                println!("==> Installing zig via dnf (best-effort)...");
                run_checked("sudo", &["dnf", "install", "-y", "zig"], None)
            } else if command_exists("pacman") {
                println!("==> Installing zig via pacman (best-effort)...");
                run_checked("sudo", &["pacman", "-S", "--noconfirm", "zig"], None)
            } else {
                Err("No supported package manager found. Install zig manually (or via zigup): https://ziglang.org/download/".into())
            }
        }
        _ => Err(format!(
            "Unsupported OS: {os}. Install zig manually (or via zigup): https://ziglang.org/download/"
        )
        .into()),
    }
}

fn run_checked(
    cmd: &str,
    args: &[&str],
    cwd: Option<&Path>,
) -> Result<(), Box<dyn std::error::Error>> {
    eprintln!("+ {} {}", cmd, args.join(" "));
    let mut c = Command::new(cmd);
    c.args(args)
        .stdin(Stdio::inherit())
        .stdout(Stdio::inherit())
        .stderr(Stdio::inherit());
    if let Some(cwd) = cwd {
        c.current_dir(cwd);
    }
    let status = c.status()?;
    if status.success() {
        Ok(())
    } else {
        Err(format!("Command failed: {cmd} {args:?} (status: {status})").into())
    }
}

fn output_checked(cmd: &str, args: &[&str]) -> Result<String, Box<dyn std::error::Error>> {
    eprintln!("+ {} {}", cmd, args.join(" "));
    let out = Command::new(cmd).args(args).output()?;
    if !out.status.success() {
        return Err(format!(
            "Command failed: {cmd} {args:?} (status: {})\nstdout:\n{}\nstderr:\n{}",
            out.status,
            String::from_utf8_lossy(&out.stdout),
            String::from_utf8_lossy(&out.stderr),
        )
        .into());
    }
    Ok(String::from_utf8_lossy(&out.stdout).to_string())
}

fn command_exists(cmd: &str) -> bool {
    Command::new("which")
        .arg(cmd)
        .stdout(Stdio::null())
        .stderr(Stdio::null())
        .status()
        .map(|s| s.success())
        .unwrap_or(false)
}

/// Clean workspace-root detection via the `cargo_metadata` crate.
///
/// Add this to xtask/Cargo.toml:
/// ```toml
/// [dependencies]
/// cargo_metadata = "0.19"
/// ```
///
/// Optionally add `anyhow = "1"` if you prefer.
fn workspace_root_via_metadata() -> Result<PathBuf, Box<dyn std::error::Error>> {
    // Ensure we run metadata from the xtask crate dir; Cargo will still find the workspace.
    let manifest_dir =
        PathBuf::from(env::var("CARGO_MANIFEST_DIR").unwrap_or_else(|_| ".".to_string()));

    let meta = cargo_metadata::MetadataCommand::new()
        .current_dir(&manifest_dir)
        .no_deps()
        .exec()?;

    Ok(meta.workspace_root.into())
}

fn parse_flag_value(args: &[String], flag: &str) -> Option<String> {
    let mut out = None;
    let mut i = 0;
    while i < args.len() {
        if args[i] == flag {
            if i + 1 < args.len() && !args[i + 1].starts_with("--") {
                out = Some(args[i + 1].clone());
                i += 2;
                continue;
            }
        }
        i += 1;
    }
    out
}
