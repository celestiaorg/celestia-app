# scan-blockresults

scan-blockresults reports which heights of a node are missing their stored block results, the
data `/block_results` serves from `state.db`. It groups the missing heights into gaps and reports
the app version of their blocks, which is what a backfill would have to re-execute.

A node can hold every block and still miss results, e.g. after running with
`discard_abci_responses = true`, after a state sync, or after restoring a store from a partial
copy. `/block` keeps working for those heights while `/block_results` fails.

## Before you start

- Stop the node or run against a copy. The stores are opened read-only, but the database lock
  still has to be free.
- The module links celestia-core v0.40.9, the version used by `celestia-appd` v9.0.x, and reads
  Pebble v1 stores. For another version, change the `replace` in `go.mod` and run `make tidy`.

## Usage

```shell
make build-linux
scp build/scan-blockresults.linux node:/tmp/scan-blockresults
/tmp/scan-blockresults --home ~/.celestia-app [--from H] [--to H] [--verify-every K] [--backend pebbledb|goleveldb]
```

`--from` and `--to` default to the blockstore base and height. With `--verify-every K`, every
K-th present result is loaded the way `/block_results` loads it and its `AppHash` and results hash
are checked against the header of block `H+1`. Results in the legacy format carry no `AppHash` and
are only checked for being loadable.

Example output (numbers are illustrative):

```text
blockstore: base=1 height=8123456
scanning heights [1, 8123456]

missing heights           count      app_version  note
[1204001 - 1204510]       510        3
[6500000 - 6500002]       3          0            block also missing

total missing results: 513 across 2 gap(s)

app versions required for backfill:
  app_version=3 : 510 heights
```

Heights marked `block also missing` cannot be regenerated from this node and are left out of the
backfill summary.
