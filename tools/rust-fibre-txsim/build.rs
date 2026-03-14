use std::fs;

fn main() {
    // Extract celestia-fibre version and commit from Cargo.lock
    let lock = fs::read_to_string("Cargo.lock").expect("failed to read Cargo.lock");

    let (version, commit) = parse_package_info(&lock, "celestia-fibre");
    println!("cargo:rustc-env=CELESTIA_FIBRE_VERSION={version}");
    println!("cargo:rustc-env=CELESTIA_FIBRE_COMMIT={commit}");
}

/// Parse version and git commit hash for a package from Cargo.lock.
///
/// Cargo.lock entries for git deps look like:
/// ```text
/// [[package]]
/// name = "celestia-fibre"
/// version = "1.0.0-rc.2"
/// source = "git+https://...#8f07565f4ae9680861c6602d112d120bef4b506e"
/// ```
fn parse_package_info(lock_contents: &str, package_name: &str) -> (String, String) {
    let mut lines = lock_contents.lines();
    while let Some(line) = lines.next() {
        if line.starts_with("name = ") && line.contains(package_name) {
            let mut version = "unknown".to_string();
            let mut commit = "unknown".to_string();
            // Read the next few lines for version and source
            for _ in 0..3 {
                if let Some(next) = lines.next() {
                    if let Some(ver) = next.strip_prefix("version = ") {
                        version = ver.trim_matches('"').to_string();
                    }
                    if let Some(src) = next.strip_prefix("source = ") {
                        let src = src.trim_matches('"');
                        if let Some(hash) = src.rsplit_once('#') {
                            commit = hash.1.to_string();
                        }
                    }
                }
            }
            return (version, commit);
        }
    }
    ("unknown".to_string(), "unknown".to_string())
}
