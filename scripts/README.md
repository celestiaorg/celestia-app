# Scripts

This directory contains a handful of scripts that may be helpful for contributors.

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

## mainnet-treedb-fast-sync-forensics.sh

Runs a mainnet state-sync with extra stuck detection/diagnostics for TreeDB fast-sync forensics.

Default output is concise and focused on progress/stuck state.

Optional TreeDB trace knobs (disabled by default):

- `TREEDB_TRACE_PATH`: enable JSONL trace output.
- `TREEDB_TRACE_SUMMARY_PATH`: optional summary JSON path (defaults to `TREEDB_TRACE_PATH.summary.json`).
- `TREEDB_TRACE_EVERY_N`: trace sampling interval (`1` = every event).
- `TREEDB_TRACE_ANALYSIS_PATH`: script-generated text diagnostics path.
- `TREEDB_TRACE_ANALYSIS_TOP_N`: top phases/iterators in diagnostics.
- `TREEDB_TRACE_ANALYSIS_LONG_ITER_MS`: threshold for long-lived iterators.
- `TREEDB_TRACE_ANALYSIS_SAMPLE_EVERY_N`: sampling stride for diagnostics parsing.
- `TREEDB_TRACE_ANALYSIS_MAX_EVENTS`: max sampled events parsed per report (`0` = unlimited).
- `TREEDB_TRACE_ANALYSIS_ON_WARN_STUCK`: include trace report in warn-stuck diagnostics (`1` to enable).
