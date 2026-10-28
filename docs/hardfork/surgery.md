# Restart a node for in-place surgery

Use this runbook when the protocol team asks you to restart your node on a release with a state fix.
The fix repairs the chain's state at one height. Every node that executes that height runs the fix, so you do not roll back or change any data.

The protocol team coordinates the surgery with [surgery-coordination.md](surgery-coordination.md).

## Quick reference

Set the variables first (see [Set variables](#set-variables)). Install the release, dry run, restart, then verify. These are the systemd commands; for cosmovisor and docker, see step 4.

```sh
bash recovery.sh restart --home "$HOME_DIR" --expect-version "$VERSION" --dry-run
bash recovery.sh restart --home "$HOME_DIR" --expect-version "$VERSION"
bash recovery.sh verify --check-height "$H" --expect-app-hash "$HASH"
```

## Before you start

The script needs `bash`, `curl` and `jq`.
Run it in a terminal as the user that runs the node. It runs `sudo systemctl`, which may ask for your password.

The restart does not touch the node's data or the signer. It sets `halt-height` and `halt-time` to 0 in `config/app.toml` and keeps the old file as `config/app.toml.bak`.
If your service sets a halt another way, with `--halt-height`, `--halt-time` or `CELESTIA_APP_HALT_HEIGHT`, remove it before the restart.

Install the release before the restart time. A node on the old release does not run the fix: it either stops at the same height again or computes a different app hash and stops one block later.

## Set variables

Take the values from the announcement. The protocol team chooses H after the restart and posts H and HASH.

```sh
HOME_DIR=<NODE_HOME>      # usually ~/.celestia-app; docker: the path inside the container
VERSION=<RELEASE_VERSION> # as printed by celestia-appd version
export DAEMON_HOME=<COSMOVISOR_HOME> # cosmovisor only; usually the node home
CONTAINER=<CONTAINER>     # docker only
H=<CHECK_HEIGHT>
HASH=<CHECK_APP_HASH>
```

## Steps

1. Get `recovery.sh` of the release in the announcement, as in step 1 of [rollback.md](rollback.md#steps).
2. Stop the node, as in step 2 of [rollback.md](rollback.md#steps). A halted node has usually exited already.
3. Install the release from the announcement, as in step 3 of [rollback.md](rollback.md#steps).
4. Run the dry run. It checks the binary version and the signer state and changes nothing. Use the line for your setup:

   ```sh
   # systemd
   bash recovery.sh restart --home "$HOME_DIR" --expect-version "$VERSION" --dry-run
   # cosmovisor
   bash recovery.sh restart --home "$HOME_DIR" --expect-version "$VERSION" --setup cosmovisor --dry-run
   # docker
   bash recovery.sh restart --home "$HOME_DIR" --expect-version "$VERSION" --setup docker --container "$CONTAINER" --dry-run
   ```

   If your systemd unit is not called `celestia-appd` (or `cosmovisor`), add `--service <unit>`.

5. Send the output to the protocol team. A good run prints `binary <VERSION>` and no `error:` line.
6. At the agreed time, run the same line as in step 4 without `--dry-run`. The script sets the halt to 0 and starts the node.
7. When the protocol team posts H and HASH, check your node:

   ```sh
   bash recovery.sh verify --check-height "$H" --expect-app-hash "$HASH"
   ```

   It waits up to 10 minutes and prints `app hash at height <H> matches`.

8. Check that your validator signs, as in step 8 of [rollback.md](rollback.md#steps).

## If a run fails or the node stops

- The script failed: fix the cause from [Messages](#messages) and run the same command again. A re-run is safe.
- The node log shows `wrong Block.Header.AppHash` after the restart: the node probably ran the old release. Check `celestia-appd version` and tell the protocol team. Do not delete any data.
- The node log shows `state fix <name> at height <N>: <cause>`: the fix failed and every node stopped at the same height. Wait for a new release from the protocol team.

## Messages

- `binary version is <V>, expected <E>`: install the release from the announcement (step 3), then run the script again.
- `+ sh -c cp ... config/app.toml`: the script sets `halt-height` and `halt-time` to 0. The old file is in `config/app.toml.bak`.
- `applied state fix` with `name` and `height` in the node log: the fix ran. This is expected once, at the fix height.
- Any other message: see [Messages in rollback.md](rollback.md#messages). The setup, flag and signer messages are the same.
