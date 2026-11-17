# Move a node to a new chain

Use this runbook when the protocol team announces a new chain started from an export of the old chain's state.
Your node keeps its keys, its signer state and its tx index. The rest of the old chain data moves aside, and the node starts from the published genesis.

The protocol team coordinates the new chain with [export-coordination.md](export-coordination.md).

## Quick reference

Set the variables first (see [Set variables](#set-variables)). Download the genesis, dry run, move the node, then verify. These are the systemd commands; for cosmovisor and docker, see step 7.

```sh
curl -fLo new-genesis.json "$GENESIS_URL"
bash recovery.sh new-chain --home "$HOME_DIR" --genesis new-genesis.json --genesis-sha256 "$GENESIS_SHA" --chain-id "$CHAIN_ID" --expect-version "$VERSION" --dry-run
bash recovery.sh new-chain --home "$HOME_DIR" --genesis new-genesis.json --genesis-sha256 "$GENESIS_SHA" --chain-id "$CHAIN_ID" --expect-version "$VERSION"
bash recovery.sh verify --chain-id "$CHAIN_ID" --check-height "$H" --expect-app-hash "$HASH"
```

## Before you start

The script needs `bash`, `curl` and `jq`.
Run it in a terminal as the user that runs the node. It runs `sudo systemctl`, which may ask for your password.

The script checks the genesis SHA-256 and chain-id, and refuses a signer state at or above the initial height of the new chain. Then it moves everything in `data/` except the keys, the signer state and `tx_index.db` to `data/recovery-backup-<first 12 characters of the SHA-256>`, deletes `config/addrbook.json`, writes `config/genesis.json`, sets `halt-height` and `halt-time` to 0 and starts the node.

Never reset the signer state: no `unsafe-reset-all`, no new `priv_validator_state.json`, no reset of a remote signer. The signer state is what stops your validator from signing an old height again.

The script supports only the default `db_dir`, `blockstore_dir`, `genesis_file` and `addr_book_file` in `config/config.toml`, and a `wal_file` in `data/`.
The genesis can be large: about 2.6 GB for mainnet. The script copies it to a temporary file (in `$TMPDIR`, usually `/tmp`) before it writes `config/genesis.json`, so leave room for two copies.

Stop every other node of the old chain that you run. If the new chain keeps the old chain-id, an old node can connect to new nodes. Keep a node on the old chain only if the protocol team asked you to run it as an archive.

## Set variables

Take the values from the announcement. The protocol team chooses H after the restart and posts H and HASH.

```sh
HOME_DIR=<NODE_HOME>      # absolute path, usually /home/<user>/.celestia-app; docker: the path inside the container
VERSION=<RELEASE_VERSION> # as printed by celestia-appd version
GENESIS_URL=<GENESIS_URL>
GENESIS_SHA=<GENESIS_SHA256>
CHAIN_ID=<CHAIN_ID>
E=<EXPORT_HEIGHT>         # only to reproduce the export
export DAEMON_HOME=<COSMOVISOR_HOME> # cosmovisor only; usually the node home
CONTAINER=<CONTAINER>     # docker only
H=<CHECK_HEIGHT>
HASH=<CHECK_APP_HASH>
```

## Steps

1. Get `recovery.sh` of the release in the announcement, as in step 1 of [rollback.md](rollback.md#steps).
2. If your node's RPC answers, send its heights and app hash to the protocol team:

   ```sh
   curl -s localhost:26657/status | jq -r .result.sync_info.latest_block_height
   curl -s localhost:26657/abci_info | jq -r '.result.response | .last_block_height, .last_block_app_hash'
   ```

3. Stop the node, as in step 2 of [rollback.md](rollback.md#steps). Stop a remote signer too. Do not start either of them on the old chain again; step 9 starts the signer for the new chain.
4. Send the height in your signer state to the protocol team:

   ```sh
   jq -r .height "$HOME_DIR/data/priv_validator_state.json"
   ```

   With a remote signer, send the last height in its state instead.

5. If the protocol team asks you to reproduce the export, export the state at E with the binary of the release in the announcement and send the SHA-256:

   ```sh
   celestia-appd export --home "$HOME_DIR" --height "$E" --output-document export.json
   sha256sum export.json
   ```

   With docker, run both commands in one-off containers with the node's volumes. The file is in the node home inside the volume:

   ```sh
   docker run --rm --volumes-from "$CONTAINER" --entrypoint celestia-appd <image> export --home "$HOME_DIR" --height "$E" --output-document "$HOME_DIR/export.json"
   docker run --rm --volumes-from "$CONTAINER" --entrypoint sha256sum <image> "$HOME_DIR/export.json"
   ```

   For mainnet the export takes a few minutes and up to 18 GB of memory. Delete `export.json` once you have sent its SHA-256.

6. When the genesis is published, install the release from the announcement, as in step 3 of [rollback.md](rollback.md#steps). Then download the genesis once:

   ```sh
   curl -fLo new-genesis.json "$GENESIS_URL"
   ```

   With docker, `--genesis` is a path on the host, not in the container.

7. Run the dry run. It checks the genesis, the keys, the signer state and the binary version, and changes nothing. Use the line for your setup:

   ```sh
   # systemd
   bash recovery.sh new-chain --home "$HOME_DIR" --genesis new-genesis.json --genesis-sha256 "$GENESIS_SHA" --chain-id "$CHAIN_ID" --expect-version "$VERSION" --dry-run
   # cosmovisor
   bash recovery.sh new-chain --home "$HOME_DIR" --genesis new-genesis.json --genesis-sha256 "$GENESIS_SHA" --chain-id "$CHAIN_ID" --expect-version "$VERSION" --setup cosmovisor --dry-run
   # docker
   bash recovery.sh new-chain --home "$HOME_DIR" --genesis new-genesis.json --genesis-sha256 "$GENESIS_SHA" --chain-id "$CHAIN_ID" --expect-version "$VERSION" --setup docker --container "$CONTAINER" --dry-run
   ```

   If your systemd unit is not called `celestia-appd` (or `cosmovisor`), add `--service <unit>`. If the announcement lists peers, add `--peers <id@host:port,...>`.

8. Send the output to the protocol team. A good run contains the following line, where I is the initial height of the new chain, and no `error:` line:

   ```text
   genesis <GENESIS_SHA>: chain-id <CHAIN_ID>, initial height <I>
   ```

9. With a remote signer, start it again before step 10, for the new chain. Keep its state: never reset it or restore an old copy. Its last height must be below I. If the chain-id changes, set the new chain-id in its config.
10. Before the genesis time, run the same line as in step 7 without `--dry-run`. The script moves the old data, starts the node and waits until the new chain reaches height I+1, up to 30 minutes after the genesis time. The node does nothing until the genesis time, and the chain starts once validators with more than 2/3 of voting power are online. The script prints:

    ```text
    old chain data moved to <HOME_DIR>/data/recovery-backup-<SHA>; delete it once the new chain runs
    chain-id <CHAIN_ID> at height <I+1>
    ```

11. When the protocol team posts H and HASH, check your node:

    ```sh
    bash recovery.sh verify --chain-id "$CHAIN_ID" --check-height "$H" --expect-app-hash "$HASH"
    ```

    It prints `app hash at height <H> matches`. Add `--rpc <url>` if the node RPC is not at `http://localhost:26657`.

12. Check that your validator signs, as in step 8 of [rollback.md](rollback.md#steps).
13. When the protocol team confirms that the chain recovered, delete the backup directory from step 10.

Full nodes, including RPC nodes of relayers, do steps 1, 3, 6 to 8, 10 and 11. The script keeps `data/tx_index.db`, which relayers need to deliver packets sent before the halt.

## If a run fails or is interrupted

- Run the same command again with the same flags. A re-run moves the old data that is left and finishes the run.
- Once the data is moved, a re-run prints `data already replaced, old data in <backup>`. It keeps the new chain data and only starts and checks the node. This is safe after the node has started on the new chain.
- `the node did not reach height <I+1>`: the new chain has not started yet. Do not run `new-chain` again for this. Check that the node runs and tell the protocol team. Later, check the node with `bash recovery.sh verify --chain-id "$CHAIN_ID" --check-height <I+1>`.

## Messages

Genesis and flags:

- `new-chain needs --genesis, --genesis-sha256 and --chain-id`, `--genesis-sha256 must be 64 hex characters` or `--peers must be id@host:port,...`: fix the flag from the announcement.
- `new-chain needs an absolute --home without . or ..`: pass the full path of the node home.
- `genesis SHA-256 is <A>, expected <B>`: the file is not the published genesis. Download it again (step 6). If the hash still differs, tell the protocol team.
- `genesis chain-id is <A>, expected <B>`: check `--chain-id` against the announcement.
- `failed to download <url>`, `cannot read <file>`, `cannot parse the genesis` or `genesis initial height is <N>`: check the path or URL and download the genesis again.

Keys and signer:

- `signer state is at height <V>, not below the initial height <I>`: do not start the node and do not reset the signer state. Tell the protocol team; the genesis must start above V.
- `cannot read the height in <file>`: the signer state file is damaged. Do not start the node and do not replace the file. Contact the protocol team.
- `warning: check by hand that the remote signer state is below height <I>`: check the last height in the remote signer's state. If it is I or above, stop and tell the protocol team.
- `node key not found: <path>` or `validator key not found: <path>`: restore the key file from your own backup and run the script again. Do not create a new key.

Layout:

- `new-chain supports only the default <key>, not "<value>"` or `new-chain needs wal_file in data/, ...`: the script can't move this layout. Contact the protocol team.
- `config path with .. is not supported: <path>`: write that path in `config/config.toml` without `..`.
- `cannot resolve <home>/data`: check that `data/` exists and is readable by the user that runs the script.

Other output:

- `start the node, then run: recovery.sh verify --chain-id <CHAIN_ID> --check-height <I+1>`: with `--setup none` the script does not start the node. Start it, then run that command.
- `the node is on chain-id <A>, expected <B>`: the node did not start from the new genesis. Check that the service uses `--home "$HOME_DIR"`, then tell the protocol team.
- Any other message: see [Messages in rollback.md](rollback.md#messages). The setup, flag and signer messages are the same.
