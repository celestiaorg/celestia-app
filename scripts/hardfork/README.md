# Hard fork recovery

`recovery.sh` rolls back or restarts a validator node, moves it to a new chain started from an export, and checks its app hash or chain-id.

See [docs/hardfork/rollback.md](../../docs/hardfork/rollback.md). Run `bash recovery.sh --help` for flags.

The binary carries the script of its release. Operators without a checkout get it with:

```bash
celestia-appd recovery-script > recovery.sh
docker run --rm --entrypoint celestia-appd <image> recovery-script > recovery.sh
```

`export.sh` syncs a stopped node to a height, exports its state there and optionally builds a candidate genesis from it. Run `bash export.sh --help` for flags.

## Test

```bash
bash scripts/hardfork/recovery_test.sh
bash scripts/hardfork/export_test.sh
```
