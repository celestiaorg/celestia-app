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
        "build-linux-gnu" => cmd_build_with_target(DEFAULT_LINUX_TARGET),
        "build-linux-musl" => cmd_build_with_target(DEFAULT_LINUX_MUSL_TARGET),
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
    println!("  setup                  Check cross-compilation prerequisites");
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
    println!("  cargo xtask build-linux");
}

/// Check if the target matches the current host platform (no cross-compilation needed).
fn is_native_target(target: &str) -> bool {
    let os = env::consts::OS;
    let arch = env::consts::ARCH;

    // On Linux x86_64, we can build for x86_64-unknown-linux-gnu natively
    os == "linux" && arch == "x86_64" && target == DEFAULT_LINUX_TARGET
}

/// Get the cross-compiler name for a target.
fn cross_compiler_for_target(target: &str) -> &'static str {
    match target {
        "x86_64-unknown-linux-gnu" => "x86_64-unknown-linux-gnu-gcc",
        "x86_64-unknown-linux-musl" => "x86_64-unknown-linux-musl-gcc",
        _ => "x86_64-unknown-linux-gnu-gcc",
    }
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

    let os = env::consts::OS;
    let cross_compiler = cross_compiler_for_target(&target);

    println!("==> Setting up cross-compilation environment...");

    if os == "macos" {
        if !command_exists("brew") {
            return Err("Homebrew not found. Install it from https://brew.sh".into());
        }

        if !command_exists(cross_compiler) {
            println!("==> Installing cross-compiler toolchain...");
            run_checked("brew", &["tap", "messense/macos-cross-toolchains"], None)?;
            run_checked("brew", &["install", &target], None)?;
        } else {
            println!("==> {cross_compiler} already installed");
        }
    } else {
        return Err(format!("Cross-compilation from {os} is not supported").into());
    }

    println!("==> Adding Rust target {target}...");
    run_checked("rustup", &["target", "add", &target], None)?;

    println!();
    println!("==> Setup complete! You can now run:");
    println!("    cargo xtask build-linux");

    Ok(())
}

fn cmd_build_linux(args: &[String]) -> Result<(), Box<dyn std::error::Error>> {
    let target =
        parse_flag_value(args, "--target").unwrap_or_else(|| DEFAULT_LINUX_TARGET.to_string());
    cmd_build_with_target(&target)
}

fn cmd_build_with_target(target: &str) -> Result<(), Box<dyn std::error::Error>> {
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
        // Cross-compilation
        let os = env::consts::OS;
        let cross_compiler = cross_compiler_for_target(target);

        if os == "macos" {
            if !command_exists(cross_compiler) {
                eprintln!();
                eprintln!("{cross_compiler} not found.");
                eprintln!("Run 'cargo xtask setup' for install instructions.");
                eprintln!();
                return Err("missing cross-compiler".into());
            }
        }

        run_checked(
            "cargo",
            &["build", "-p", PACKAGE_NAME, "--release", "--target", target],
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
    let mut i = 0;
    while i < args.len() {
        if args[i] == flag && i + 1 < args.len() && !args[i + 1].starts_with("--") {
            return Some(args[i + 1].clone());
        }
        i += 1;
    }
    None
}
