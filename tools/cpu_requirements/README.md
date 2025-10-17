# CPU Requirements Benchmarking Tool for 128MB/6s Upgrade

## Overview

This tool tests whether your hardware is compatible with the **128MB/6s** upgrade for Celestia validator nodes. It benchmarks your system's CPU performance by measuring the time it takes to execute the consensus steps with 127 transactions of 1MB each.

## What It Does

1. Runs 20 iterations of the consensus steps with a 128MB block (with 127 x 1MB transactions)
2. Calculates the **median** time for each operation
3. Compares your results against reference times required for 128MB/6s
4. Provides a clear recommendation on whether your hardware is ready for the upgrade

## Requirements

- Must run on **Linux** (CPU feature detection requires `/proc/cpuinfo`)
- Go 1.24 or higher

## Usage

```bash
go run ./tools/cpu_requirements
```

## Output

The tool displays:
- Progress for each iteration
- Median execution time for each operation in milliseconds
- Comparison to reference times (faster/slower with multiplier)
- Final assessment with hardware upgrade recommendations if needed

## Hardware Requirements for 128MB/6s

If your system is slower than reference times, you need to upgrade to:
- **32 CPU cores** (or more)
- CPUs with **GFNI** (Galois Field New Instructions) support
- CPUs with **SHA-NI** (SHA New Instructions) support

These are the minimum requirements to handle the 128MB/6s block throughput.

## Configuration

Adjust these constants in `main.go`:
- `benchmarkIterations`: Number of test runs (default: 20)
- `referencePrepareProposalMs`, etc.: Reference times in milliseconds
