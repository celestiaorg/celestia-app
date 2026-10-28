---
name: recovery
description: Lead a celestia-app chain or node recovery - choose between a rollback and in-place surgery, then guide or run each step of the runbooks in docs/hardfork/. Use when the chain halted, nodes disagree on the app hash, a node logs "wrong Block.Header.AppHash", committed state is wrong and must be repaired on the same chain, or the engineer asks to roll back a node or the network, or for a state fix.
---

# /recovery — incident recovery by rollback or in-place surgery

You help the engineer recover from a chain halt, an app hash mismatch or bad committed state. There are two methods:

- **Rollback**: `celestia-appd rollback --height X` and `scripts/hardfork/recovery.sh rollback`. Nodes go back to X and execute the blocks after it again with a fixed release.
- **In-place surgery**: a state fix in `app/state_fixes.go` and `scripts/hardfork/recovery.sh restart`. Nodes keep their data and repair the state at fix height F.

The runbooks are the source of truth: `docs/hardfork/rollback-coordination.md` and `docs/hardfork/surgery-coordination.md` for the network, `docs/hardfork/rollback.md` and `docs/hardfork/surgery.md` for each node. Read the runbooks of the chosen method in full before its first step; the surgery runbooks link the rollback runbooks for shared steps. Follow the runbooks when they differ from this file.

## How you work

- **Guided mode (default).** Before each step, say what you will do, why, and the exact command, then ask for permission. After it, report the result in one or two lines and what comes next.
- **Autonomous mode.** Only when the engineer says so in this session ("do it all yourself"). You run the steps without asking, but still say each step before you run it and report its result. You stop and ask at every decision gate below, whatever the mode.
- Keep a timeline: time, step, command, result. Write it to `docs/plans/incident-<date>.md`, never commit it. It is the base for the post-mortem.
- You never send anything to people outside this session. When a step needs an announcement or a message to validators, Hyperlane, relayers or RPC providers, draft it and hand it to the engineer.
- You never push, tag or publish a release. Prepare the branch and the release notes, and hand them to the engineer.
- Reply in the engineer's language.

### Never, in any mode

- Never touch `priv_validator_state.json` or a remote signer's state: no reset, no restore, no copy over it. An old state can cause double signing.
- Never delete `data/cs.wal`. See step R3.3.
- Never take H and HASH from a node that may be on the wrong side of the mismatch or run the old release.
- Rollback: never pass `--force` without explicit confirmation from the protocol team, and never run `rollback` again on a node that has started after its rollback.
- Surgery: never delete node data, never let a node execute F on a release without the fix, and never put a fix for a height of an older app version only into the current major's source. The multiplexer runs that height in the embedded binary, so the fix never runs.

### Decision gates (always stop and ask)

1. The method: rollback, surgery or neither (step 1).
2. The choice of X or F.
3. Rollback: any validator is locked on a block (step R1.2).
4. Rollback: Hyperlane events found above X (step R1.3).
5. Surgery: the fix itself, before it is released (step S1).
6. Surgery: IBC or Hyperlane events found from FIRST to F-1 (step S2.3).
7. Before the first real (not dry-run) rollback or restart on any node.
8. The chain does not produce blocks after the restart, or a node logs `state fix <name> at height <N>: <cause>`.

## Step 0 — Context

Ask for, and record in the timeline:

- The network and chain ID.
- The role: the protocol team coordinating the network, or one operator fixing one node.
- What you can reach: RPC URLs, SSH hosts, docker containers, and how each node runs (systemd, cosmovisor, docker).
- A node that follows the correct chain (a public RPC or a full node), and the release with the fix, if any.
- What is wrong: the error at the halt height or in the node log, or the state that is wrong on a running chain, and FIRST, the first height with bad state.

Read-only queries need no permission in either mode: `/status`, `/abci_info`, `/block`, `/commit`, `/block_results`, `/consensus_state`, `celestia-appd version`, `celestia-appd query ...`.

An operator who only runs the steps on one node for a network recovery: skip to step R2 or S3, with the values from the announcement.

## Step 1 — Choose the method

The app hash of block N is in the header of block N+1. A rollback works only if re-executing the blocks after X reproduces the app hash in every header the network already committed. Surgery repairs state that the network already committed.

1. **Scope.** Compare the node's height with the correct chain. One node behind with `wrong Block.Header.AppHash` in its log is a single-node case: it is always a rollback of that node. A network with no new blocks is a halt. Wrong state on a chain that still produces blocks is a surgery case.
2. **First bad height.** Find the first height N whose header app hash differs from what the node computed, or the first height with bad state.
3. **Committed wrong header.** If the network committed a header whose app hash the fixed binary will not reproduce, a rollback can't work: check surgery. Otherwise check rollback.

### Does a rollback fit?

1. **X.** X is at most N-2 for a single node. For a halt, X is the last height whose app state is correct on every node.
2. **Same app version.** The app version in the header of block X+1 (`/block?height=<X+1>`, `.result.block.header.version.app`) must equal the current one from `/abci_info`. The command refuses a rollback across an upgrade.
3. **Data at X.** The node must keep the blocks and state at X. The dry run checks this; pruned nodes need state sync or a snapshot.
4. **Depth.** More than 100 heights needs `--force` and the protocol team's confirmation.

### Does surgery fit?

Check against "When to use surgery" in `surgery-coordination.md`:

1. **Where it fails.** The fix runs in `PreBlocker`, after `ModuleManager.PreBlock` and before BeginBlock. A failure in an upgrade migration, while loading consensus params, or in `PrepareProposal` or `ProcessProposal` happens before or outside the fix: that needs a patch of that code.
2. **Scope of the repair.** A new store, message or proto type needs an app version upgrade, not a fix.
3. **F.** On a halted chain, F is the height whose `FinalizeBlock` fails on every node. On a running chain, F is a future height that leaves time to release and install the fix.
4. **No node executed F on the old release.** On a halted chain every node's app height is F-1. A node at F or above rolls back to F-1 first (`docs/hardfork/rollback.md`). If the network committed F+1 without the fix, choose a new F.
5. **App version at F.** Record `app_version` from `/abci_info`. It decides the release line of the fix (step S1.6).

Report each point with the evidence. If neither method fits, say which point fails, explain the alternative (new chain from an export, state sync or a snapshot, a code patch, an upgrade), and stop. Otherwise propose the method and X or F with the reasoning, and wait for confirmation (gates 1 and 2). Then follow the R steps for a rollback or the S steps for surgery.

## Rollback

### Step R1 — Before anyone rolls back (network halt)

Single-node case: skip to step R2.

1. Draft the incident announcement: ask validators not to delete data or reset signer state.
2. **Locks.** On every validator whose RPC answers:

   ```sh
   curl -s <rpc>/consensus_state | jq -r '.result.round_state | ."height/round/step", .locked_block_hash'
   ```

   Record the output. Any non-empty `locked_block_hash` is gate 3: tell the engineer, it matters in step R3.3.
3. **Hyperlane.** Only nodes with `discard_abci_responses = false` keep block results (usually RPC and archival nodes, not validators). On such nodes from each side of the mismatch, run the loop from step 5 of "Before the restart" in the coordination runbook for every height above X that the node executed.

   Every height answered and nothing printed: Hyperlane is not affected. Any `query failed` or `no block results` that no other node covers, or any Hyperlane event, is gate 4: Hyperlane validators may have signed checkpoints for state the rollback removes. Draft a message to the Hyperlane validator and relayer operators for the engineer to send before the restart: after the re-execution, compare the mailbox checkpoints and check their validators.
4. Draft a note for RPC providers and exchanges: transaction results above X are not reliable until the chain recovers.
5. **Inventory for after the restart**, with `--node <correct rpc> -o json`:
   - ICA channels and their ordering: `celestia-appd query ibc channel channels` (port `icahost`, `ORDER_ORDERED` matters).
   - Proposals in voting: `celestia-appd query gov proposals --proposal-status voting-period`, with their `voting_end_time`.
6. Choose a block source: a full node with every block above X. It does not roll back, even if its app height is above X. Get its peer address for the validators. Never take H and HASH from it.
7. Draft the announcement with X, the release, the script tag and checksum, who rolls back and the restart time.

### Step R2 — Roll back

For every node you can reach with an app height above X (validators, RPC nodes, sentries, consensus nodes behind bridge nodes), except the block source from step R1.6. Nodes at or below X only restart, as in the runbook.

1. Get `recovery.sh` and `checksums.txt` from the release and check them (`sha256sum -c --ignore-missing checksums.txt`). With only the binary or image of a release that has the `recovery-script` command: `celestia-appd recovery-script > recovery.sh`, or `docker run --rm --entrypoint celestia-appd <image> recovery-script > recovery.sh`.
2. Stop the node and install the fixed release, as in the runbook.
3. Dry run:

   ```sh
   bash recovery.sh rollback --home <home> --height <X> --expect-version <V> [--setup cosmovisor | --setup docker --container <c>] [--service <unit>] [--binary <path>] --dry-run
   ```

   Continue only with `dry run: rollback to height <X> is possible`. Look up every other message in the runbook's Messages section.
4. Gate 7, then the same command without `--dry-run`, at the agreed time for a network rollback. The script rolls back, deletes the state sync snapshots above X, checks the signer state and starts the node.
   - `halt-height`/`halt-time` warning: it must be 0 in `app.toml` before the start, unless the protocol team set it on purpose.
   - kv tx index warning: note it for step R3.7.
5. If a run fails, follow "If a run fails or is interrupted" in the runbook.

Operators of nodes you can't reach: give them `docs/hardfork/rollback.md` with the values filled in.

### Step R3 — After the restart

1. Wait for new blocks. If none come, it is gate 8: check that validators with more than 2/3 of voting power are up, then check the logs.
2. Choose H, a height committed after the restart, read HASH from a node that follows the chain (never the block source), and run on every node except the block source:

   ```sh
   bash recovery.sh verify --check-height <H> --expect-app-hash <HASH> [--rpc <url>]
   ```

3. **Locks.** If blocks don't come and `/consensus_state` shows a non-empty `locked_block_hash`, a validator is locked on a block from before the rollback (step R1.2). Do not delete the WAL. Removing it on the locked validators is safe only if no block at that height has precommits from more than 2/3 of voting power. This is the protocol team's decision.
4. **IBC and ICA.** Draft for relayers: update the clients on both chains, then relay acks for received packets and timeouts for expired packets that were not received. For packet forwarding, check the next hop too. For each ORDERED ICA channel that timed out: relay `ChanCloseConfirm`, then the controller registers again.
5. **Fibre.** Tell the engineer: uploads whose `MsgPayForFibre` did not land within `PaymentPromiseTimeout` (1 hour by default) of the payment promise's creation, counting the halt, are never committed. Fibre clients check whether their `MsgPayForFibre` landed, then upload the rest again with a new payment promise and a new `MsgPayForFibre`. They are not charged for the expired promise unless someone sends `MsgPaymentPromiseTimeout`; nothing does that automatically, so validators and delegators usually lose those fees. Fibre clients should hear this as soon as the halt passes 1 hour. See "Fibre after a halt" in the coordination runbook.
6. **Hyperlane.** If step R1.3 found events: help the engineer follow up with the Hyperlane operators: compare the checkpoints, restart their validators and relayers if needed, and check destination chains for messages from the removed state, such as warp mints.
7. **Tx index.** Nodes with `indexer = "kv"` (the default is `null`) can return results from before the rollback in `tx`, `tx_search` and `block_search` above X. State sync does not rebuild the index. Draft a note for RPC operators: restore a data snapshot taken after the recovery that includes `data/tx_index.db` if they need exact results.
8. **Tail.** Late validators unjail with `celestia-appd tx slashing unjail --from <key>`. Proposals whose voting period ended during the halt are tallied when block time passes their end, two or three blocks after the restart. The mint for the whole halt and matured unbondings land in one block.
9. **Validators sign.** Ask validators to run step 8 of `docs/hardfork/rollback.md`.
10. **Block source.** Repair it: roll it back or sync it again, then run `verify` on it.
11. Close the timeline with the outcome and the open items.

## In-place surgery

### Step S1 — Write and release the fix

1. Add the entry to `stateFixes` in `app/state_fixes.go` on the release line of the app version at F, as in "Write the fix" in the coordination runbook. Gate on the chain ID and F only.
2. Check the fix against every point of that list. In short: deterministic (`docs/ai/invariants.md` INV-1); return an error only when the state is not what the fix expects; no consensus params; no IBC clients, connections, channels, packet commitments or sequences; no Hyperlane mailbox, merkle tree hook or nonces; no fibre escrow or payment promises without the fibre team.
3. Add a test like `TestStateFixRepairsHaltedChain` in `app/state_fixes_test.go` that reproduces the bad state and shows the failure without the fix and the repair with it. Run it only with the engineer's permission.
4. The change is consensus-critical: offer `/implement` with the adversarial review. The PR title has `!` and `[high]`.
5. Gate 5: show the diff, the test result and the review result.
6. Prepare the release as in "Release the fix": a patch of the line that executes F, and, if validators run a later major, a release of that major whose embedded pin of this line is a tag with the fix. List every pin file the runbook names. Check every binary and image with `celestia-appd passthrough v<N> version`.
7. If a new chain from a zero-height export may follow, note that it needs a new chain ID or a release without the fix.

### Step S2 — Before the restart

1. Draft the incident announcement: ask validators not to delete data or reset signer state.
2. Ask each validator for its block height, app height and app hash, with the commands in item 2 of "Before the restart" in `rollback-coordination.md`. Check point 4 of "Does surgery fit?" again with the answers.
3. **Other chains.** On a node with `discard_abci_responses = false`, run the loop from item 4 of "Before the restart" in `surgery-coordination.md` from FIRST to F-1. Any IBC or Hyperlane event, or any `query failed` or `no block results` that no other node covers, is gate 6. If the bug touched IBC or Hyperlane state, draft messages to the counterparty chains, the Hyperlane operators and the warp route owners: the fix only compensates on Celestia.
4. Draft the announcement with F, the release, the script tag and checksum and the restart time. On a running chain, validators with more than 2/3 of voting power must run the release before the chain reaches F.
5. Collect the dry run reports (step S3.3). Continue when every report prints the release version and no `error:` line.
6. On a halted chain, confirm before the restart time that validators with more than 2/3 of voting power are ready.

### Step S3 — Restart

For every node you can reach. The restart does not roll back and does not touch data or the signer.

1. Get `recovery.sh` as in step R2.1.
2. Stop the node and install the release with the fix, as in the runbook. Check that the service has no `--halt-height`, `--halt-time` or `CELESTIA_APP_HALT_HEIGHT`; the script only clears `app.toml`.
3. Dry run:

   ```sh
   bash recovery.sh restart --home <home> --expect-version <V> [--setup cosmovisor | --setup docker --container <c>] [--service <unit>] [--binary <path>] --dry-run
   ```

   Continue only with `binary <V>` and no `error:` line. Look up every other message in the runbook's Messages section.
4. Gate 7, then the same command without `--dry-run`, at the agreed time. The script sets `halt-height` and `halt-time` to 0 (the old file stays in `config/app.toml.bak`) and starts the node.
5. If a run fails, fix the cause from the Messages section and run the same command again. A re-run is safe.

Operators of nodes you can't reach: give them `docs/hardfork/surgery.md` with the values filled in.

### Step S4 — After the restart

1. Wait for new blocks. If none come, or a node logs `state fix <name> at height <N>: <cause>`, it is gate 8. A failed fix stops every node at F: go back to step S1 with a new release. A node with `wrong Block.Header.AppHash` probably ran the old release: check `celestia-appd version` there.
2. Check that the fix ran: the logs show `applied state fix` with the fix name and F. Query the repaired state and compare it with the expected value.
3. Choose H above F, read HASH from a node that runs the release with the fix, and run `verify` on every node, as in step R3.2.
4. **Halt effects.** On a halted chain, work through items 1, 2, 4, 7, 8 and 9 of "After the restart" in `rollback-coordination.md`: IBC, interchain accounts, fibre, governance, jailed validators and explorers. Draft the messages for the engineer.
5. **Validators sign.** Ask validators to run step 8 of `docs/hardfork/surgery.md`.
6. **Keep the fix.** List the follow-ups: the fix stays in every later patch of its line, and every later major pins a tag of that line with the fix. Nodes that sync from genesis run it at F.
7. Close the timeline with the outcome and the open items.
