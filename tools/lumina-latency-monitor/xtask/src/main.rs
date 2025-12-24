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
    println!("  setup                  Set up cross-compilation (zig/zigbuild on non-Linux)");
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

/// Check if the target matches the current host platform (no cross-compilation needed).
fn is_native_target(target: &str) -> bool {
    let os = env::consts::OS;
    let arch = env::consts::ARCH;

    // On Linux x86_64, we can build for x86_64-unknown-linux-gnu natively
    if os == "linux" && arch == "x86_64" && target == DEFAULT_LINUX_TARGET {
        return true;
    }

    false
}

fn cmd_setup(args: &[String]) -> Result<(), Box<dyn std::error::Error>> {
    let target =
        parse_flag_value(args, "--target").unwrap_or_else(|| DEFAULT_LINUX_TARGET.to_string());

    // If we're on native Linux x64, no setup needed for gnu target
    if is_native_target(&target) {
        println!("==> Running on native Linux x86_64, no cross-compilation setup needed");
        println!();
        println!("==> You can now run:");
        println!("    cargo xtask build-linux");
        return Ok(());
    }

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
    let workspace_root = workspace_root_via_metadata()?;

    println!("==> Building {BIN_NAME} for {target}...");

    // If we're on native Linux x64 building for gnu target, use plain cargo build
    if is_native_target(target) {
        run_checked(
            "cargo",
            &["build", "-p", PACKAGE_NAME, "--release"],
            Some(&workspace_root),
        )?;

        let binary_path = workspace_root.join("target").join("release").join(BIN_NAME);

        println!("==> Built: {}", binary_path.display());
    } else {
        // Cross-compilation: use zigbuild
        if !command_exists("cargo-zigbuild") {
            return Err("cargo-zigbuild not found. Run 'cargo xtask setup' first.".into());
        }

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
    }

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
                Err(
                    "Homebrew not found. Install zig manually: https://ziglang.org/download/"
                        .into(),
                )
            }
        }
        "linux" => {
            // On Linux, we only need zig for cross-compilation (e.g., to musl)
            if command_exists("apt-get") {
                println!("==> Installing zig via apt...");
                run_checked("sudo", &["apt-get", "update"], None)?;
                run_checked("sudo", &["apt-get", "install", "-y", "zig"], None)
            } else if command_exists("dnf") {
                println!("==> Installing zig via dnf...");
                run_checked("sudo", &["dnf", "install", "-y", "zig"], None)
            } else if command_exists("pacman") {
                println!("==> Installing zig via pacman...");
                run_checked("sudo", &["pacman", "-S", "--noconfirm", "zig"], None)
            } else {
                Err("No supported package manager found. Install zig manually: https://ziglang.org/download/".into())
            }
        }
        _ => Err(format!(
            "Unsupported OS: {os}. Install zig manually: https://ziglang.org/download/"
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

fn workspace_root_via_metadata() -> Result<PathBuf, Box<dyn std::error::Error>> {
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
