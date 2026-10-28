# Coordinate a network rollback

This runbook is for the protocol team. Validators and node operators follow [rollback.md](rollback.md).

## When to use a rollback

The app hash of block N is stored in the header of block N+1.
A rollback works only if re-executing the blocks after X reproduces the app hashes in every header the network has already committed.

Use a rollback when:

- One node, or a minority of nodes, computed a wrong app hash, and the rest of the network has the correct headers.
- The chain halted before it committed a header with the wrong app hash. For example, validators disagreed on the app hash and no block got more than 2/3 of voting power.

Do not use a rollback when:

- The network committed a header with a wrong app hash. A rollback can't fix it; use a new chain from an export.
- The target height is before an app version upgrade. Rollback works only within one app version, and the command refuses this case. Use in-place surgery or a new chain.
- The node pruned the state or blocks at the target height. Use state sync or a snapshot.

## Before the restart

1. Announce the incident in the validator channels. Ask validators not to delete data or reset signer state.
2. Ask each validator for its block height, app height and app hash, if its RPC answers:

   ```sh
   curl -s localhost:26657/status | jq -r .result.sync_info.latest_block_height
   curl -s localhost:26657/abci_info | jq -r '.result.response | .last_block_height, .last_block_app_hash'
   ```

   `/abci_info` prints the app hash in base64, and `/block` prints it in hex. Compare hashes in the same encoding.

3. Choose X: the last height whose app state is correct on every node.
4. Record which validators are locked on a block, if their RPC answers. Save the output for item 3 of [After the restart](#after-the-restart):

   ```sh
   curl -s localhost:26657/consensus_state | jq -r '.result.round_state | ."height/round/step", .locked_block_hash'
   ```

   An empty `locked_block_hash` means the validator is not locked. This is the usual case when validators disagreed on the app hash.

5. Check Hyperlane. Only nodes with `discard_abci_responses = false` in `config/config.toml` keep block results; these are usually RPC and archival nodes, not validators. On such nodes from each side of the mismatch, list the Hyperlane events for every height above X that the node executed. Set LAST to the node's last executed height:

   ```sh
   for N in $(seq $((X + 1)) "$LAST"); do
     r=$(curl -fsS "localhost:26657/block_results?height=$N") || { echo "$N: query failed"; continue; }
     echo "$r" | jq -r --arg n "$N" 'if .result == null then "\($n): no block results" else ([.result.txs_results[]?.events[]?, .result.finalize_block_events[]?] | map(.type | select(startswith("hyperlane."))) | unique[] | "\($n): \(.)") end'
   done
   ```

   - Every height answered and nothing printed: the rollback does not affect Hyperlane.
   - Any `query failed` or `no block results` that no other node covers: treat Hyperlane as affected.
   - Any Hyperlane event: Hyperlane validators may have signed checkpoints for state that the rollback removes. Before the restart, tell the Hyperlane validator and relayer operators. After the re-execution, they compare the mailbox checkpoints and check their validators, and recover them if the checkpoints differ.

6. Tell RPC providers and exchanges that transaction results above X are not reliable until the chain recovers. See item 6 of [After the restart](#after-the-restart).
7. Decide whether the network still produces blocks. If it does, validators that roll back go one at a time, and each runs the dry run and the rollback in one go. This keeps their downtime short.
8. Choose the release with the fix, if one is needed.
9. Keep at least one full node with every block above X as a block source. It does not roll back, even if its app height is above X. Without it, rolled-back nodes stall at the first deleted height. Share its peer address, so validators can add it to `persistent_peers` in `config/config.toml`. Its app state may be wrong too, so do not take H and HASH from it. Repair it after the network recovers.
10. All other nodes with an app height above X roll back. The others only restart.
11. Announce X, the release, who rolls back and the restart time.
12. If the network is halted, ask every validator that rolls back to run the dry run (steps 1-5 of [rollback.md](rollback.md#steps)). Continue only when every report contains `dry run: rollback to height X is possible`.
13. Before the restart time, confirm that validators with more than 2/3 of voting power are ready. Otherwise the chain does not produce blocks.
14. At the restart time, validators run the rollback or restart.

## Confirm the chain recovered

1. Check that blocks are produced. Run this twice; the height must grow:

   ```sh
   curl -s localhost:26657/status | jq -r .result.sync_info.latest_block_height
   ```

2. Choose H: a height the network committed after the restart. On any node that follows the chain, read HASH, the app hash in the header of block H, and post H and HASH:

   ```sh
   curl -s "localhost:26657/block?height=$H" | jq -r .result.block.header.app_hash
   ```

3. Check that nodes agree on the app hash. Run this on several nodes. Skip the block source until it is repaired:

   ```sh
   bash recovery.sh verify --check-height "$H" --expect-app-hash "$HASH"
   ```

4. Ask validators to run step 8 of [rollback.md](rollback.md#steps) to check that they sign.

## After the restart

Work through this list in the first hour after blocks are produced again.
Block time jumps over the halt at the second or third block after the restart, not at the first one.

1. IBC. Relayers update the clients on both chains, then relay acknowledgements for packets that were received and timeouts for packets that expired without being received. Transfers whose timestamp timeout passed during the halt time out; the default ICS-20 timeout is 10 minutes. For packet forwarding, check the next hop too: its timeout or retry ends with an acknowledgement relayed back.
2. Interchain accounts. A timeout closes an ORDERED ICA channel. Relay `ChanCloseConfirm` to Celestia; then the controller registers the account again and gets the same address. List the ICA channels and their controllers before the restart.
3. The chain does not produce blocks and `/consensus_state` shows a non-empty `locked_block_hash` on some validators (the command in step 4 of [Before the restart](#before-the-restart)). These validators are locked on a block from before the rollback. Do not delete `data/cs.wal`. The lock is what stops the network from committing two blocks at one height. The protocol team decides; removing the WAL on the locked validators is safe only if no block at that height has precommits from more than 2/3 of voting power.
4. Fibre. Uploads whose `MsgPayForFibre` did not land in time are never committed; the clients upload that data again. See [Fibre after a halt](#fibre-after-a-halt).
5. Hyperlane. If step 5 of [Before the restart](#before-the-restart) found Hyperlane events, follow up with the Hyperlane operators: compare the checkpoints, restart their validators and relayers if needed, and check the destination chains for messages from the removed state, for example warp mints.
6. Nodes with `indexer = "kv"` in `config/config.toml` (the default is `null`). The tx index is not rolled back, so `tx`, `tx_search` and `block_search` can return results from before the rollback for heights above X. State sync does not rebuild the index. If the node must serve exact results, restore a data snapshot taken after the recovery that includes `data/tx_index.db`.
7. Governance. Proposals whose voting period ended during the halt are tallied when block time passes their end, two or three blocks after the restart, so voters have almost no time to vote after the restart. Tell voters in advance if the halt is long.
8. Validators that restarted late may be jailed for downtime. They unjail with `celestia-appd tx slashing unjail --from <key>`.
9. Tell explorers to expect the mint for the whole halt in one block, and unbondings that matured during the halt completing at once.
10. Repair the block source from step 9 of [Before the restart](#before-the-restart): roll it back or sync it again, then run `verify` on it.

## Fibre after a halt

A fibre upload is committed to the chain only when its `MsgPayForFibre` lands in a block: that transaction puts the system blob with the data commitment in the square. The chain accepts `MsgPayForFibre` only within `PaymentPromiseTimeout` of the payment promise's creation, in block time (1 hour by default; check the current fibre params). Block time jumps over the halt two or three blocks after the restart, so the halt counts toward that hour.

After the restart, clients first check whether their `MsgPayForFibre` landed before the halt, then send the pending ones again. A promise that has expired by then fails with `payment promise expired`, or with `creation_timestamp ... is too old` if it is older than `WithdrawalDelay` (24 hours by default). Its upload is never committed, even though the fibre servers hold the data until they prune it. The client must upload the data again with a new payment promise and send a new `MsgPayForFibre`. Tell fibre clients this as soon as the halt passes 1 hour.

The client is not charged for the expired promise, unless someone settles it with `MsgPaymentPromiseTimeout` while it is less than `WithdrawalDelay` old. Nothing sends that message automatically, so validators and delegators usually do not get the fees for these promises.
