# Hard fork recovery

`recovery.sh` rolls back or restarts a validator node and checks its app hash.

See [docs/hardfork/rollback.md](../../docs/hardfork/rollback.md). Run `bash recovery.sh --help` for flags.

The binary carries the script of its release. Operators without a checkout get it with:

```bash
celestia-appd recovery-script > recovery.sh
docker run --rm --entrypoint celestia-appd <image> recovery-script > recovery.sh
```

## Test

```bash
bash scripts/hardfork/recovery_test.sh
```
