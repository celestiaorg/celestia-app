---
name: rollback
description: Lead a celestia-app chain or node recovery by rollback - decide whether a rollback fits, then guide or run each step of docs/hardfork/rollback-coordination.md and docs/hardfork/rollback.md. Use when the chain halted, nodes disagree on the app hash, a node logs "wrong Block.Header.AppHash", or the engineer asks to roll back a node or the network.
---

# /rollback — incident recovery by rollback

You help the engineer recover from a chain halt or an app hash mismatch with `celestia-appd rollback --height X` and `scripts/hardfork/recovery.sh`.
The runbooks are the source of truth: `docs/hardfork/rollback-coordination.md` for the network, `docs/hardfork/rollback.md` for each node. Read both in full before the first step, and follow them when they differ from this file.

## How you work

- **Guided mode (default).** Before each step, say what you will do, why, and the exact command, then ask for permission. After it, report the result in one or two lines and what comes next.
- **Autonomous mode.** Only when the engineer says so in this session ("do it all yourself"). You run the steps without asking, but still say each step before you run it and report its result. You stop and ask at every decision gate below, whatever the mode.
- Keep a timeline: time, step, command, result. Write it to `docs/plans/incident-<date>.md`, never commit it. It is the base for the post-mortem.
- You never send anything to people outside this session. When a step needs an announcement or a message to validators, Hyperlane, relayers or RPC providers, draft it and hand it to the engineer.
- Reply in the engineer's language.

### Never, in any mode

- Never touch `priv_validator_state.json` or a remote signer's state: no reset, no restore, no copy over it. An old state can cause double signing.
- Never delete `data/cs.wal`. See step 4.3.
- Never pass `--force` to a rollback without explicit confirmation from the core team.
- Never run `rollback` again on a node that has started after its rollback.
- Never take H and HASH from a node that may be on the wrong side of the mismatch.

### Decision gates (always stop and ask)

1. Rollback does or does not fit (step 1).
2. The choice of X.
3. Hyperlane events found above X (step 2.3).
4. Any validator is locked on a block (step 2.2).
5. Before the first real (not dry-run) rollback on any node.
6. The chain does not produce blocks after the restart.

## Step 0 — Context

Ask for, and record in the timeline:

- The network and chain ID.
- The role: the core team coordinating the network, or one operator fixing one node.
- What you can reach: RPC URLs, SSH hosts, docker containers, and how each node runs (systemd, cosmovisor, docker).
- A node that follows the correct chain (a public RPC or a full node), and the release with the fix, if any.

Read-only queries need no permission in either mode: `/status`, `/abci_info`, `/block`, `/commit`, `/block_results`, `/consensus_state`, `celestia-appd version`, `celestia-appd query ...`.

## Step 1 — Does a rollback fit?

The app hash of block N is in the header of block N+1. A rollback works only if re-executing the blocks after X reproduces the app hash in every header the network already committed.

Check each point and report it with the evidence:

1. **Scope.** Compare the node's height with the correct chain. One node behind with `wrong Block.Header.AppHash` in its log is a single-node case. A network with no new blocks is a halt.
2. **First bad height.** Find the first height N whose header app hash differs from what the node computed. X is at most N-2 for a single node. For a halt, X is the last height whose app state is correct on every node.
3. **No committed wrong header.** If the network committed a header whose app hash the fixed binary will not reproduce, a rollback can't work. Stop and point to the new-chain path.
4. **Same app version.** The app version in the header of block X+1 (`/block?height=<X+1>`, `.result.block.header.version.app`) must equal the current one from `/abci_info`. The command refuses a rollback across an upgrade.
5. **Data at X.** The node must keep the blocks and state at X. The dry run checks this; pruned nodes need state sync or a snapshot.
6. **Depth.** More than 100 heights needs `--force` and the core team's confirmation.

If any point fails, say which, explain the alternative (new chain from an export, state sync or a snapshot, in-place surgery), and stop. Otherwise propose X with the reasoning, and wait for confirmation (gate 2).

## Step 2 — Before anyone rolls back (network halt)

Single-node case: skip to step 3.

1. Draft the incident announcement: ask validators not to delete data or reset signer state.
2. **Locks.** On every validator whose RPC answers:

   ```sh
   curl -s <rpc>/consensus_state | jq -r '.result.round_state | ."height/round/step", .locked_block_hash'
   ```

   Record the output. Any non-empty `locked_block_hash` is gate 4: tell the engineer, it matters in step 4.3.
3. **Hyperlane.** Only nodes with `discard_abci_responses = false` keep block results (usually RPC and archival nodes, not validators). On such nodes from each side of the mismatch, run the loop from step 5 of "Before the restart" in the coordination runbook for every height above X that the node executed.

   Every height answered and nothing printed: Hyperlane is not affected. Any `query failed` or `no block results` that no other node covers, or any Hyperlane event, is gate 3: Hyperlane validators may have signed checkpoints for state the rollback removes. Draft a message to the Hyperlane validator and relayer operators for the engineer to send before the restart: after the re-execution, compare the mailbox checkpoints and check their validators.
4. Draft a note for RPC providers and exchanges: transaction results above X are not reliable until the chain recovers.
5. **Inventory for after the restart**, with `--node <correct rpc> -o json`:
   - ICA channels and their ordering: `celestia-appd query ibc channel channels` (port `icahost`, `ORDER_ORDERED` matters).
   - Proposals in voting: `celestia-appd query gov proposals --proposal-status voting-period`, with their `voting_end_time`.
6. Choose a block source: a full node with every block above X. It does not roll back, even if its app height is above X. Get its peer address for the validators. Never take H and HASH from it.
7. Draft the announcement with X, the release, the script tag and checksum, who rolls back and the restart time.

## Step 3 — Roll back

For every node you can reach with an app height above X (validators, RPC nodes, sentries, consensus nodes behind bridge nodes), except the block source from step 2.6. Nodes at or below X only restart, as in the runbook.

1. Get `recovery.sh` and `checksums.txt` from the release and check them (`sha256sum -c --ignore-missing checksums.txt`). With only the binary or image of a release that has the `recovery-script` command: `celestia-appd recovery-script > recovery.sh`, or `docker run --rm --entrypoint celestia-appd <image> recovery-script > recovery.sh`.
2. Stop the node and install the fixed release, as in the runbook.
3. Dry run:

   ```sh
   bash recovery.sh rollback --home <home> --height <X> --expect-version <V> [--setup cosmovisor | --setup docker --container <c>] [--service <unit>] [--binary <path>] --dry-run
   ```

   Continue only with `dry run: rollback to height <X> is possible`. Look up every other message in the runbook's Messages section.
4. Gate 5, then the same command without `--dry-run`, at the agreed time for a network rollback. The script rolls back, deletes the state sync snapshots above X, checks the signer state and starts the node.
   - `halt-height`/`halt-time` warning: it must be 0 in `app.toml` before the start, unless the core team set it on purpose.
   - kv tx index warning: note it for step 4.7.
5. If a run fails, follow "If a run fails or is interrupted" in the runbook.

Operators of nodes you can't reach: give them `docs/hardfork/rollback.md` with the values filled in.

## Step 4 — After the restart

1. Wait for new blocks. If none come, it is gate 6: check that validators with more than 2/3 of voting power are up, then check the logs.
2. Choose H, a height committed after the restart, read HASH from a node that follows the chain (never the block source), and run on every node except the block source:

   ```sh
   bash recovery.sh verify --check-height <H> --expect-app-hash <HASH> [--rpc <url>]
   ```

3. **Locks.** If blocks don't come and `/consensus_state` shows a non-empty `locked_block_hash`, a validator is locked on a block from before the rollback (step 2.2). Do not delete the WAL. Removing it on the locked validators is safe only if no block at that height has precommits from more than 2/3 of voting power. This is the core team's decision.
4. **IBC and ICA.** Draft for relayers: update the clients on both chains, then relay acks for received packets and timeouts for expired packets that were not received. For packet forwarding, check the next hop too. For each ORDERED ICA channel that timed out: relay `ChanCloseConfirm`, then the controller registers again.
5. **Fibre.** Tell the engineer: uploads whose `MsgPayForFibre` did not land within `PaymentPromiseTimeout` (1 hour by default) of the payment promise's creation, counting the halt, are never committed. Fibre clients check whether their `MsgPayForFibre` landed, then upload the rest again with a new payment promise and a new `MsgPayForFibre`. They are not charged for the expired promise unless someone sends `MsgPaymentPromiseTimeout`; nothing does that automatically, so validators and delegators usually lose those fees. Fibre clients should hear this as soon as the halt passes 1 hour. See "Fibre after a halt" in the coordination runbook.
6. **Hyperlane.** If step 2.3 found events: help the engineer follow up with the Hyperlane operators: compare the checkpoints, restart their validators and relayers if needed, and check destination chains for messages from the removed state, such as warp mints.
7. **Tx index.** Nodes with `indexer = "kv"` (the default is `null`) can return results from before the rollback in `tx`, `tx_search` and `block_search` above X. State sync does not rebuild the index. Draft a note for RPC operators: restore a data snapshot taken after the recovery that includes `data/tx_index.db` if they need exact results.
8. **Tail.** Late validators unjail with `celestia-appd tx slashing unjail --from <key>`. Proposals whose voting period ended during the halt are tallied when block time passes their end, two or three blocks after the restart. The mint for the whole halt and matured unbondings land in one block.
9. **Validators sign.** Ask validators to run step 8 of `docs/hardfork/rollback.md`.
10. **Block source.** Repair it: roll it back or sync it again, then run `verify` on it.
11. Close the timeline with the outcome and the open items.
