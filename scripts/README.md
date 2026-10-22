# Scripts

This directory contains a handful of scripts that may be helpful for contributors.

## test_cover.sh

Run `make test-coverage` from the repository root to generate `coverage.txt`.
The script runs the full test suite serially, excluding `test/util` packages
from test selection. Cross-package tests contribute coverage to the packages
they exercise. Packages without tests remain in scope.
Duplicate profile blocks are combined so their statements are counted once.

Only production code is in scope. The script does not instrument `test/`,
`tools/`, `scripts/`, `local_devnet/` or `pkg/rsema1d/cmd/testvectors`, and drops
generated `*.pb.go` and `*.pb.gw.go` files. `.github/codecov.yml` ignores the
same paths, so local and Codecov reports agree.

The report is replaced only after all tests pass. A failed run leaves an existing
report unchanged, so check the command's exit status before using the report.
Run `bash scripts/test_cover_test.sh` for a small regression check of the script.

CI splits the suite into shards on separate runners. `COVER_SHARD=<name> make test-coverage`
runs one shard, and `bash scripts/test_cover.sh --list-shards` lists them. The
`rest` shard runs every package that no named shard owns. Every shard instruments
the full scope, so `bash scripts/merge_coverage.sh coverage.txt <profiles>` combines
shard profiles into the same report as a full run.
See [coverage reports and staged checks](../docs/coverage.md) for the global rule,
proposal exceptions, the handler execution assertion, and the accept/reject invariant matrix.

## build-run-single-node.sh

This script will build the project and run a single node devnet. After running this script, the text output will contain a "Home directory" that you can use as a parameter for subsequent commands.

```bash
./scripts/build-run-single-node.sh
Home directory: /var/folders/_8/ljj6hspn0kn09qf9fy8kdyh40000gn/T/celestia_app_XXXXXXXXXXXXX.XV92a3qx
--> Updating go.mod
...
```

In a new terminal tab, export the home directory:

```bash
export CELESTIA_APP_HOME=/var/folders/_8/ljj6hspn0kn09qf9fy8kdyh40000gn/T/celestia_app_XXXXXXXXXXXXX.XV92a3qx
```

In subsequent commands, pass the `--home $CELESTIA_APP_HOME` flag:

```bash
./build/celestia-appd keys list validator --home $CELESTIA_APP_HOME
- address: celestia1grvklux2yjsln7ztk6slv538396qatckqhs86z
  name: validator
  pubkey: '{"@type":"/cosmos.crypto.secp256k1.PubKey","key":"A5R27GO4uGtzu7LVOxneiA3i59Bi7SlDr6FHaGfy47mI"}'
  type: local
```

Note: this script is used in <https://github.com/celestiaorg/docs> so please update the docs repo if you make breaking changes to this script.
