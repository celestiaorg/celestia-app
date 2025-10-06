# TxClient Stress Test

This tool repeatedly broadcasts blob transactions to **Mocha node** with short TTLs and monitors the Celestia TxClient to ensure it never halts.

## What it does

- Submits random blob transactions at a fixed interval (`intervalMs`).
- Tracks the time since the last successful broadcast.
- Fails if no successful submission happens for more than 10 seconds.
- Runs for `testDurationSec` seconds by default, then exits cleanly.

## How to run

Before running the script you will have to set up a keyring directory with an address that has mocha tia on it.

```bash
 go run tools/spam_txclient/main.go
 ```
