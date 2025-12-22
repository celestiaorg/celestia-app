# Blockchain Network Metrics Monitor

A real-time monitoring tool for tracking and analyzing blockchain network performance metrics including block times, sizes, and throughput.

## Overview

This tool connects to a blockchain node via RPC and monitors network performance by tracking:

- Block production times
- Block sizes
- Network throughput

The metrics are calculated over a sliding window of blocks to provide current network performance insights.

## Features

- Real-time block monitoring
- Rolling window metrics calculation (default 100 blocks)
- Thread-safe metrics collection
- Periodic metrics reporting (every 10 seconds)
- Key metrics tracked:
  - Average block time
  - Average block size
  - Network throughput in MB/s
  - Total blocks processed

## Usage

You can run the tool using:

```shell
 go run tools/throughput/main.go https://celestia-rpc.publicnode.com:443

 go run tools/throughput/main.go https://celestia-mocha-rpc.publicnode.com:443
```
