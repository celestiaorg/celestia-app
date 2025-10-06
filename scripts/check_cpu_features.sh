#!/bin/bash

# Script to check CPU features for celestia-app
# Checks for GFNI and SHA_NI CPU features that improve cryptographic performance

check_cpu_features() {
    local warning="
CPU Performance Warning: Missing hardware acceleration features

Your CPU does not support one or more of the following hardware acceleration features:
- GFNI (Galois Field New Instructions)
- SHA_NI (Secure Hash Algorithm New Instructions)

These features significantly improve cryptographic performance for blockchain operations.

Note: These features are not required for the 32MB/6s block configuration but will become
essential when the network transitions to 128MB/6s blocks. Validators should prepare by upgrading
their hardware to ensure optimal performance during future network upgrades.

To check what features your CPU supports:
grep -o -E 'sha_ni|gfni' /proc/cpuinfo

Modern Intel CPUs (10th gen+) and AMD CPUs (Zen 3+) typically support these features.
If you are running this node in production, consider upgrading to a CPU with these features.

This node will continue to run, but may experience reduced performance for cryptographic operations.
"

    # Only check on Linux where /proc/cpuinfo is available
    if [[ "$OSTYPE" != "linux-gnu"* ]]; then
        # Skip check silently for non-Linux OSes (e.g., macOS, Windows, BSD)
        return 0
    fi

    # Check if /proc/cpuinfo exists and is readable
    if [[ ! -f /proc/cpuinfo ]] || [[ ! -r /proc/cpuinfo ]]; then
        echo "Warning: Could not read /proc/cpuinfo to check CPU features"
        return 0
    fi

    # Check for CPU features
    local cpu_features
    cpu_features=$(grep -o -E 'sha_ni|gfni' /proc/cpuinfo 2>/dev/null | sort -u)
    
    local has_gfni=false
    local has_sha_ni=false
    local missing_features=()

    if echo "$cpu_features" | grep -q "gfni"; then
        has_gfni=true
    else
        missing_features+=("GFNI")
    fi

    if echo "$cpu_features" | grep -q "sha_ni"; then
        has_sha_ni=true
    else
        missing_features+=("SHA_NI")
    fi

    # If any features are missing, show warning
    if [[ ${#missing_features[@]} -gt 0 ]]; then
        echo "$warning"
        printf "Missing features: %s\n\n" "$(IFS=', '; echo "${missing_features[*]}")"
    fi

    return 0
}

# Run the check if script is executed directly
if [[ "${BASH_SOURCE[0]}" == "${0}" ]]; then
    check_cpu_features
fi
