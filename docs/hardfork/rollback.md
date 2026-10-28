# Rollback a node

Use this runbook when the core team asks you to roll back your node, or when your node logs `wrong Block.Header.AppHash`.
A rollback moves the node's state back to a target height X. On restart, the node re-executes the blocks after X with the binary it runs.

The core team coordinates a network rollback with [rollback-coordination.md](rollback-coordination.md).

## Quick reference

Set the variables first (see [Set variables](#set-variables)). Dry run, rollback, then verify. These are the systemd commands; for cosmovisor and docker, see step 4.

```sh
bash recovery.sh rollback --home "$HOME_DIR" --height "$X" --expect-version "$VERSION" --dry-run
bash recovery.sh rollback --home "$HOME_DIR" --height "$X" --expect-version "$VERSION"
bash recovery.sh verify --check-height "$H" --expect-app-hash "$HASH"
```

## Before you start

The script needs `bash`, `curl` and `jq`.
Run it in a terminal as the user that runs the node. It runs `sudo systemctl`, which may ask for your password.

A rollback does not touch the signer. The node re-executes blocks it gets from peers and never signs those heights again.
The rollback deletes the node's blocks above X+1, and the node fetches them again from peers.
A rollback deeper than 100 heights needs `--force`.

A single node with a wrong app hash logs `wrong Block.Header.AppHash` while validating block N. Its state at N-1 is wrong, so X is N-2.
This holds only if N is the first height with a mismatch. If you are not sure, ask the core team for X.
If the cause is unknown, first collect evidence with [app-hash-mismatch.md](../maintainers/app-hash-mismatch.md).

## Set variables

Take the values from the announcement. The core team chooses H after the restart and posts H and HASH.

```sh
HOME_DIR=<NODE_HOME>      # usually ~/.celestia-app; docker: the path inside the container, /home/celestia/.celestia-app in the release images
X=<TARGET_HEIGHT>
VERSION=<RELEASE_VERSION> # as printed by celestia-appd version
export DAEMON_HOME=<COSMOVISOR_HOME> # cosmovisor only; usually the node home
CONTAINER=<CONTAINER>     # docker only
H=<CHECK_HEIGHT>
HASH=<CHECK_APP_HASH>
```

## Steps

1. Download the script from the release in the announcement and verify it:

   ```sh
   curl -LO https://github.com/celestiaorg/celestia-app/releases/download/<tag>/recovery.sh
   curl -LO https://github.com/celestiaorg/celestia-app/releases/download/<tag>/checksums.txt
   sha256sum -c --ignore-missing checksums.txt
   ```

   If you have only the binary or the image, a release that has the `recovery-script` command prints its script:

   ```sh
   celestia-appd recovery-script > recovery.sh
   docker run --rm --entrypoint celestia-appd <image> recovery-script > recovery.sh
   ```

   Use the binary or image of the release in the announcement. Run `bash recovery.sh --help` for all flags.

2. Stop the node. A halted node has usually exited already; stopping it again is harmless. The dry run needs a stopped node, because a running node locks its databases.

   ```sh
   sudo systemctl stop celestia-appd   # systemd
   sudo systemctl stop cosmovisor      # cosmovisor
   docker stop "$CONTAINER"            # docker
   ```

3. If the announcement names a fixed release, install it:

   ```sh
   sudo install -m 0755 <fixed-binary> "$(command -v celestia-appd)"                  # systemd
   install -m 0755 <fixed-binary> "$DAEMON_HOME/cosmovisor/current/bin/celestia-appd"  # cosmovisor
   ```

   With systemd, the binary must be the one in `ExecStart` (`systemctl cat celestia-appd | grep ExecStart`). If that is not the `celestia-appd` on your PATH, add `--binary <path>` to the commands below: the script checks the version of the binary it runs.

   With docker, make sure the node home (data, config and signer state) is on a volume or bind mount. Otherwise recreating the container loses it.
   Then recreate the container from the fixed image without starting it, for example with `docker compose up --no-start`.

4. Run the dry run. It runs every rollback check and does not roll anything back. Use the line for your setup:

   ```sh
   # systemd
   bash recovery.sh rollback --home "$HOME_DIR" --height "$X" --expect-version "$VERSION" --dry-run
   # cosmovisor
   bash recovery.sh rollback --home "$HOME_DIR" --height "$X" --expect-version "$VERSION" --setup cosmovisor --dry-run
   # docker
   bash recovery.sh rollback --home "$HOME_DIR" --height "$X" --expect-version "$VERSION" --setup docker --container "$CONTAINER" --dry-run
   ```

   If your systemd unit is not called `celestia-appd` (or `cosmovisor`), add `--service <unit>`.

5. Send the output to the core team. A good run contains the following, where S is the node's current height:

   ```text
   rollback: height <S> -> <X>, blocks to delete: <M>
   dry run: rollback to height <X> is possible
   ```

6. At the agreed time, run the same line as in step 4 without `--dry-run`. Add `--force` only if the core team confirmed it. The script runs the checks again, rolls back, deletes the state sync snapshots above X and starts the node:

   ```sh
   # systemd
   bash recovery.sh rollback --home "$HOME_DIR" --height "$X" --expect-version "$VERSION"
   # cosmovisor
   bash recovery.sh rollback --home "$HOME_DIR" --height "$X" --expect-version "$VERSION" --setup cosmovisor
   # docker
   bash recovery.sh rollback --home "$HOME_DIR" --height "$X" --expect-version "$VERSION" --setup docker --container "$CONTAINER"
   ```

   It prints `Rolled back to height <X>, app hash <APP_HASH>`.

7. When the core team posts H and HASH, check your node:

   ```sh
   bash recovery.sh verify --check-height "$H" --expect-app-hash "$HASH"
   ```

   It waits up to 10 minutes and prints `app hash at height <H> matches`.
   Add `--rpc <url>` if the node RPC is not at `http://localhost:26657`.
   For a single node, choose H above the height where your node stopped. Take HASH from the header of block H on any node that follows the chain.

8. Check that your validator signs. Run this twice, a minute apart. Both runs must print `signed`:

   ```sh
   ADDR=$(curl -s localhost:26657/status | jq -r .result.validator_info.address)
   curl -s localhost:26657/block | jq -r --arg a "$ADDR" '.result.block.last_commit.signatures[] | select(.block_id_flag == 2 and .validator_address == $a) | "signed"'
   ```

Validators that only restart do steps 2 and 3, check that `halt-height` and `halt-time` are 0 in `config/app.toml`, then start the node at the agreed time with `sudo systemctl start <unit>` or `docker start "$CONTAINER"`.

## If a run fails or is interrupted

This applies only while the node has not run since the rollback.

1. Run the same command again with the same flags. A re-run usually finishes an interrupted rollback.
2. If the re-run fails, restore the node with state sync or a snapshot. There is no backup. This can happen when a rollback of more than one height was interrupted.

Once the node has started after the rollback, do not run `rollback` again. It would roll back to X again and delete newer blocks. Run `verify` (step 7) instead, or contact the core team.

## Messages

Look up a message here by its text.

Setup and flags:

- `rollback check skipped: the node is running; stop it to run the check`: nothing was checked. Stop the node and run the dry run again.
- `run as the owner of <home> (uid <N>), not as root`: run the script again as that user.
- `rollback needs --height, a positive number`: pass `--height "$X"`.
- `--setup docker needs --container`: add `--container "$CONTAINER"`.
- `celestia-appd not found on PATH; use --binary`: add `--binary <path>`.
- `the node did not stop`: stop it by hand and run the script again.
- An error about a database lock, such as `resource temporarily unavailable`: the node still runs, often under another unit name. Add `--service <unit>` and run the script again.
- `less than 1 GiB free in <home>/data`: free disk space and run the script again.
- `cannot parse config.toml: <line>`: fix the quoting on that line of `config/config.toml` and run the script again.
- `binary version is <V>, expected <E>`: install the release from the announcement (step 3), then run the script again.

Rollback checks:

- `rollback check failed`: the line above it has the cause. Look it up in this list.
- `rollback depth <D> exceeds 100; use --force`, or the same with `app rollback depth`: check X. Add `--force` only if the core team confirms it.
- `rollback across an upgrade is not supported`: X is before an app version upgrade. Stop and tell the core team.
- `node is on app version <N>, binary on <M>: use "celestia-appd passthrough ...`: the node is on an older app version. Stop and contact the core team.
- `node is on app version <N>, binary on <M>: use a newer celestia-appd`: install the release from the announcement.
- `target height <X> out of range [<base>, <S>]`: the node has no state at X. Tell the core team.
- `store <name> has no version <X>` or `block meta at height <N> not found`: the node pruned data the rollback needs. Use state sync or a snapshot.
- `validator set at height <N>: <cause>` or `consensus params at height <N>: <cause>`: the node may have pruned data the rollback needs. Send the full error to the core team before you use state sync or a snapshot.
- `app hash at height <X> is <A>; CometBFT expects <B>`: the local app state does not match the committed header. Stop and tell the core team.

Signer:

- `signer state not found: <path>`: the node uses a local signer, but its state file is missing. Do not start the node. Contact the core team. Never create a new file or restore an old copy: an old copy forgets later votes and can cause double signing.
- `signer state changed since <home>/data/recovery-signer.sha256 was saved; do not start the node`: do not start the node. Contact the core team.
- `warning: signer state not checked; check the remote signer by hand`: check that the signer is running and its state wasn't changed. Never reset it.

Other output:

- `warning: app.toml sets halt-height = <N>; the node stops again there` (or `halt-time`): set it to 0 in `config/app.toml` before the node starts, unless the core team asked for it.
- `warning: the kv tx index keeps results from before the rollback; ...`: `tx`, `tx_search` and `block_search` above X can return results from before the rollback. Tell the core team if this node serves RPC.
- `+ .../celestia-appd snapshots delete ...`: the script deletes a state sync snapshot above X. It holds the state from before the rollback, and peers that restore it fail state sync.
- The node fails to start with `rollback to height <T> is not finished; run the rollback command again with --height <T>`: a rollback was interrupted. This is expected. Run step 6 again with the same flags and `--height <T>`.
