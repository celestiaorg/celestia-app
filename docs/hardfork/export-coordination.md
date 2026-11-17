# Coordinate a new chain from an export

This runbook is for the protocol team. Validators and node operators follow [export.md](export.md).

A new chain starts from the state of the old chain at export height E. We export the state on our own node and build the genesis. Validators keep their keys and signer state and start from the published genesis. The old chain's blocks stay on archive nodes.

## When to use a new chain

Use a new chain when the state is too broken for a rollback or in-place surgery. See [When to use a rollback](rollback-coordination.md#when-to-use-a-rollback) and [When to use surgery](surgery-coordination.md#when-to-use-surgery). Check [Known limitations](#known-limitations) first.

A new chain repairs nothing by itself: the genesis tool copies the state at E as it is. Broken state is left behind only by exporting below the height where it went wrong. E is then the last height whose state is correct.

## Decisions for the new chain

- **Chain-id.** An emergency restart keeps the chain-id and leaves a height gap. IBC continues through a one-shot Hermes update against an archive node; see [IBC](#ibc).
  A planned restart uses a new revision of the chain-id through an IBC upgrade proposal; see [Planned restart](#planned-restart).
  A new chain-id without a proposal is a separate, explicit decision to give up IBC continuity; see [New chain-id without a proposal](#new-chain-id-without-a-proposal).
- **Heights.** E is the export height. C is the highest height with a commit on any node (`latest_block_height` in `/status`). V is the highest height in any validator's signer state. The initial height of the new chain is I = max(V, C, E) + 11. The new chain then never has a header or vote at a height where an old one exists, so old votes can't become double-sign evidence. With the same chain-id, the genesis tool refuses I below E + 11.
- **Export height.** Export at the tip, the last height that nodes executed (`last_block_height` in `/abci_info`), whenever its state is correct. Transactions in blocks above E never happen on the new chain, and other chains may have already seen them; see [Export below the tip](#export-below-the-tip).
- **Signer state.** Validators keep it. `recovery.sh new-chain` refuses a local signer state at or above I. Never reset it.
- **Genesis time.** G is the planned start time, and the first block of the new chain has that time. G can't be earlier than the block time of E. Nothing is minted for the halt, and the inflation schedule continues from the original genesis time.
- **Binary.** The new chain runs the exported app version. Use the same release binary for the export, the genesis tool and every validator. The genesis records the release in `app_name` and `app_version`.
- **Signal.** x/signal state is not exported. Validators signal again for any upgrade after the restart.
- **DA.** Bridge and light nodes of celestia-node need their own procedure, which the celestia-node team handles separately.

## Before the restart

1. Announce the incident in the validator channels, as in item 1 of [Before the restart](rollback-coordination.md#before-the-restart). Also ask validators not to start nodes on the old chain again once they stop them.
2. Ask each validator for its block height, app height and app hash while its RPC answers, with the commands in item 2 of [Before the restart](rollback-coordination.md#before-the-restart). Take C and E from the answers.
3. Ask every validator to stop its node and remote signer and send its signer height (steps 3 and 4 of [export.md](export.md#steps)). Publish a deadline. V is the highest height sent. An honest validator signs at most C + 1, so investigate any higher V before the start. After the deadline, continue without the missing validators: if they are honest and stopped, their signer state is at most C + 1, which I covers.
4. Emergency restart: compute I = max(V, C, E) + 11. A planned restart uses the I fixed in its proposal; see [Planned restart](#planned-restart).
5. Keep at least two archive nodes of the old chain. Use full nodes without a validator key that hold every block up to C and ran with `discard_abci_responses = false` and `indexer = "kv"`. Do not prune them, and do not move them to the new chain. Before G, isolate them in `config/config.toml` and restart them: `persistent_peers = ""`, `seeds = ""`, `pex = false`, `max_num_inbound_peers = 0` and `max_num_outbound_peers = 0`. Keep them up after the restart: Hermes and the Hyperlane relayer read old blocks from them. If C is above E, Hermes needs one with every block up to C, and the Hyperlane relayer one trimmed to E (see [Hyperlane](#hyperlane)). A planned restart sets up its archives differently.
6. Choose the release and G. Leave time to publish the genesis, download it (about 2.6 GB for mainnet) and run the dry runs.
7. Run the export script (not merged yet, #8668) from a checkout of the release tag, on a stopped node of ours that has already executed E. The script can sync a node that is behind, but on a halted chain it may not reach E: block sync verifies block E only with block E+1. The script runs the node with a throwaway signer, so it never signs, exports the state, hashes it and builds the genesis with `celestia-appd genesis from-export` (not merged yet, #8669). HOME_DIR is the node home, OUT an empty output directory and ARCHIVE_RPC an archive node's RPC:

   ```sh
   bash scripts/hardfork/export.sh --home "$HOME_DIR" --height "$E" --rpc "$ARCHIVE_RPC" --out "$OUT" \
     --genesis-time "$G" --initial-height "$I"
   ```

   Add `--chain-id <id>` only for a new chain-id. The script writes these files to `--out`: `export.json`, `export.json.sha256`, `export.manifest`, `header.json`, `node.log`, `export.log`, `timings.tsv` and `summary.txt`, and, when it builds the genesis, `genesis.json`, `genesis.json.sha256`, `genesis.json.report.json` and `genesis.log`.

8. Check `app_hash_at_height` in `summary.txt`. The script compares the node's app hash at E with the header of block E+1 on `--rpc`, and stops before the export on a mismatch. `matches block <E+1>: <hash>` is the good case. `not verified (<rpc> has no block <E+1>)`, `not verified (node.log has no finalized block <E>)` and `not verified (the app is at <N>)` mean the script could not compare. Then compare the node's app hash at E with the ones validators sent in item 2. Read it in `<out>/node.log`: the `finalized block` line for height E (`block_app_hash`), or, if the node was already at E, the `ABCI Handshake App Info` line (`hash`). Both are info-level lines, so they need `log_level` info, the default. If `node.log` has neither, the export reproduction in item 11 is the check. The log and `summary.txt` print hex, and `/abci_info` prints base64; compare hashes in the same encoding.
9. Review the report. Its `status` says the candidate may be published only after the checks in this runbook pass, items 8 to 12.
   - `chain_id`, `initial_height` and `genesis_time` change from the export to the values you chose. `chain_id_warning` appears only with a new chain-id.
   - `export_height` is E and `app_version` the release.
   - `validators` and `total_power` list the validator set of the new chain.
   - `checks` lists the supply, module and escrow balances, their surpluses and the mint excess. Compare them with the old chain at E, for example `celestia-appd query bank total --height "$E" --node "$ARCHIVE_RPC"`.
10. If the script stops with `genesis from-export failed; see <out>/genesis.log` and the log shows `conservation: "mint": "excess": want "<= 0 or acknowledged", got "<X>"`, check the excess. A positive excess is normal after a slash or burn since the yearly inflation recompute. Once reviewed, run item 7 again with `--accept-mint-excess <X>`.
11. Ask validators with more than 2/3 of voting power to reproduce the export (step 5 of [export.md](export.md#steps)). Their SHA-256 must equal ours in `export.json.sha256`. The export takes only `genesis_time`, `chain_id` and `app_hash` from the node's `config/genesis.json` (cosmos-sdk `server/export.go`); the rest comes from the state and the binary. So the hashes match when validators run the same release binary on a node with the chain's genesis file. Do not publish the genesis until it does; if a hash differs, find out why first.
12. At the same time, dry start the candidate. This runs InitChain on it. Use a host where no other node runs, with about 64 GB of memory for mainnet, and an empty directory SCRATCH with room for the state:

    ```sh
    celestia-appd init dry-start --home "$SCRATCH" --chain-id "$CHAIN_ID"
    cp "$OUT/genesis.json" "$SCRATCH/config/genesis.json"
    celestia-appd start --home "$SCRATCH" --force-no-bbr --p2p.laddr tcp://127.0.0.1:26656 --p2p.persistent_peers "" --p2p.seeds "" --p2p.pex=false >dry-start.log 2>&1 &
    PID=$!
    ```

    It passes when `dry-start.log` shows `Completed ABCI Handshake - CometBFT and App are synced` and then `Genesis time is in the future. Sleeping until then...`. It fails if the process exits, or the log shows `panic` or `error during handshake`. The RPC does not start before G, so `/status` and `/abci_info` don't answer. Then stop the node and delete the scratch home. The node ignores SIGTERM while it waits for G, so use `kill -9`:

    ```sh
    kill -9 "$PID"
    rm -rf "$SCRATCH"
    ```

13. Publish the genesis URL, its SHA-256 from `genesis.json.sha256`, the chain-id, I, G, the release, persistent peers and the export SHA-256. Post the SHA-256 in the validator channels, not only next to the file.
14. Tell relayer operators what to do; see [IBC](#ibc) and [Hyperlane](#hyperlane).
15. Ask every validator to run the dry run (steps 6-8 of [export.md](export.md#steps)). Continue when every report contains the genesis line and no `error:` line.
16. Before G, confirm that validators ready to start hold more than 2/3 of `total_power` in the report. They run `recovery.sh new-chain` before G, and their nodes wait for G.

If an export script run fails or is interrupted, run it again with the same `--out`, `--height` and chain. It reuses `export.json` if it matches `export.json.sha256` and skips the sync and the export. It reuses `genesis.json` only if it was built with the same values. To build the genesis with other values, delete `genesis.json`, `genesis.json.sha256` and `genesis.json.report.json` first. If it says `<out> is not empty` or `remove <out>`, use an empty `--out`. If it says `stop it first`, stop that node.

## Confirm the chain recovered

1. Check that blocks are produced, as in item 1 of [Confirm the chain recovered](rollback-coordination.md#confirm-the-chain-recovered). The first block is I:

   ```sh
   curl -s localhost:26657/status | jq -r .result.sync_info.earliest_block_height
   ```

2. Check that the first blocks carry no double-sign evidence. This prints 0 for each height:

   ```sh
   for N in $(seq "$I" $((I + 2))); do curl -s "localhost:26657/block?height=$N" | jq '.result.block.evidence.evidence | length'; done
   ```

3. Choose H above I, post H and HASH, and check several nodes, as in items 2 and 3 of [Confirm the chain recovered](rollback-coordination.md#confirm-the-chain-recovered). Add `--chain-id "$CHAIN_ID"` to `verify`.
4. Ask validators to run step 12 of [export.md](export.md#steps) to check that they sign.
5. Send an IBC transfer both ways, from Celestia with `celestia-appd tx ibc-transfer transfer transfer <channel> <receiver> <amount> --from <key>` and back from the counterparty.
6. Send a warp transfer both ways, from Celestia with `celestia-appd tx warp transfer <token-id> <destination-domain> <recipient> <amount> --max-hyperlane-fee <fee> --from <key>` and back from the EVM chain.
7. Submit a new blob and check that it is included in a block. Query its transaction on an RPC node with a tx index; it must print a height and code `0`:

   ```sh
   celestia-appd tx blob pay-for-blob 0x00010203040506070809 0x48656c6c6f --from <key> --chain-id <chain-id> --fees <fee> --node <rpc> --yes
   celestia-appd query tx <txhash> --node <rpc> -o json | jq -r '.height, .code'
   ```

## After the restart

Work through items 1, 2, 7 and 8 of [After the restart](rollback-coordination.md#after-the-restart): IBC timeouts, interchain accounts, governance and jailed validators. Block time jumps to G at the first block. Fibre differs from a rollback; see item 5 below.

Then:

1. Tell explorers and indexers that the new chain starts at I. Heights E+1 to I-1 do not exist on it, and older blocks are only on the archives. Nothing is minted for the halt. Unbondings that matured during the halt complete in the first block.
   After an export below the tip, the kept tx index still returns transactions from heights E+1 to C.
2. Ask validators to signal again for any pending upgrade.
3. Keep the archive nodes up. Devops runs them: the archive with every block up to C (Hermes, the damage assessment above E, undelivered Hyperlane messages, DA blobs below E) and, when needed, the one rolled back to E for the Hyperlane relayer. Keep them for at least 30 days and at least the trusting period of the counterparty IBC clients, and until the damage assessment and the list of undelivered messages are closed. Then take a final snapshot of each and keep it in cold storage.
4. Tell validators when they may delete their backups (step 13 of [export.md](export.md#steps)).
5. Fibre. The genesis carries no staking historical info, so the new chain can't verify a certificate signed for an old height, even with the same chain-id. A `MsgPayForFibre` that did not land before the halt fails. Tell fibre clients to upload that data again, with a new payment promise and certificate at a height of the new chain. Settling an old promise with `MsgPaymentPromiseTimeout` needs no validator set. With the same chain-id it works while the promise's creation time is less than `WithdrawalDelay` (24 hours by default) before the block time, which starts at G. With a new chain-id it fails: the promise names the old chain-id.

## IBC

With the same chain-id, the IBC clients of Celestia on other chains continue. `MsgUpdateClient` is permissionless, so we run the update ourselves:

1. Relayer operators stop Hermes before G. The Celestia RPC node of a relayer moves with `recovery.sh new-chain`, which keeps `data/tx_index.db`. Hermes needs it to relay packets sent before the halt.
2. List the clients to update. On Celestia, `celestia-appd query ibc connection connections` shows each connection's `counterparty.client_id`. The counterparty chain is the `chain_id` in `celestia-appd query ibc client state <client_id>`.
3. Once the new chain produces blocks, update every client. `--restart-height` and `--archive-address` only route fetches: Hermes reads light blocks at or below C from the archive and higher ones from the live RPC. It needs the full light block at the trusted height T plus one. If T is C, block C+1 is in the height gap and the update fails, so pass an older T. On the counterparty (HOST_CHAIN, HOST_RPC), read the client's latest height and its stored consensus heights; follow `pagination.next_key` to see them all:

   ```sh
   <counterpartyd> query ibc client state "$CLIENT" --node "$HOST_RPC" -o json
   <counterpartyd> query ibc client consensus-state-heights "$CLIENT" --node "$HOST_RPC" --limit 1000 -o json
   ```

   Pick T, the newest stored height below C whose consensus state is still within the trusting period. Do not assume C-1 is stored. Then update the client to I:

   ```sh
   hermes update client --host-chain "$HOST_CHAIN" --client "$CLIENT" --trusted-height "$T" --height "$I" --restart-height "$C" --archive-address "$ARCHIVE_RPC"
   ```

   If no stored height below C is usable, stock Hermes v1.13.1 can't update the client; see [Open questions](#open-questions). Changing only `--height` does not help, and an update through C recreates the problem.

4. Check on each counterparty that `query ibc client status <client-id>` prints `Active` and that the client's latest height is at least I. Then start Hermes. A packet sent before the halt is delivered once. Transfers whose timeout passed during the halt time out, as after a rollback.

An expired client can't be updated. Each counterparty then recovers it by governance with `MsgRecoverClient`. Celestia's own clients of other chains also age by the halt, because the new chain starts at G.

### New chain-id without a proposal

This gives up IBC continuity. Choose it only as an explicit decision:

- Every client of Celestia on every counterparty stops. It can't follow a new chain-id.
- Transfers and interchain accounts wait until the client expires (its trusting period) and a `MsgRecoverClient` proposal with a new substitute client passes on each counterparty.
- Escrowed funds stay in the state of both chains.
- Hyperlane is not affected.

Relayers point Hermes at the new chain-id: rename the chain in the Hermes config and copy its key directory under `.hermes/keys/` to the new name.

## Hyperlane

Hyperlane state does not depend on the chain-id and carries over: mailboxes, the delivered set and collateral. A message delivered before the halt can't be delivered again.

Relayer operators run these steps; we tell them when. Hyperlane validators that sign for Celestia read the old chain from their own old node, never from ours: an archive can make a validator sign a forged checkpoint.

When E is the tip, the relayer keeps its database:

1. Point the relayer's Celestia endpoints (`rpcUrls`, `grpcUrls`, `restUrls`) at an archive node and restart it. It delivers the messages sent before the halt. If C is above E, use an archive trimmed to E: roll it back to E with [rollback.md](rollback.md), then isolate it again. This case is not tested.
2. Wait until `hyperlane_contract_sync_block_height` reaches E for every Celestia data type. Switching earlier silently drops events.
3. Point the endpoints at the new chain, set `index.from` of the Celestia chain to I, and restart the relayer.

When E is below the tip, the relayer starts with a fresh database:

1. Move the old database aside. Do not delete it and never reuse it: it skips the nonces that new messages reuse, keeps old leaves in its merkle tree, and its cursor can stall below I.
2. Start the relayer on the new chain with an empty database and `index.from` set to I.
3. Once `hyperlane_contract_sync_stored_events` for `dispatched_messages` on Celestia shows the first messages from the new chain, restart it once.

## Edge cases

### Export below the tip

Use this only when the state at the tip is wrong. Before the restart, list the IBC packets and Hyperlane messages from heights E+1 to C on an archive with `tx_search`. Tell the counterparty chains and the warp route owners, and pause the affected warp routes.

The e2e tests `TestIBCExportBelowTip` and `TestHyperlaneExportBelowTip` showed this for traffic in the lost blocks:

- **Celestia to counterparty, received.** The counterparty keeps the vouchers, but Celestia no longer escrows the tokens: they are unbacked. Recovery: compensation through governance.
- **Counterparty to Celestia, received and acknowledged.** Lost. The counterparty escrows the tokens and has no commitment left, so the packet can't be relayed again. Celestia never minted the vouchers. Recovery: compensation through governance.
- **Counterparty to Celestia, received but not acknowledged.** The counterparty still has the commitment. Relaying it again delivers it once.
- **New Celestia transfers on a reused sequence.** The counterparty's receive is a no-op, and a timeout fails with `failed packet receipt absence verification`. Anyone can relay the old acknowledgement, which deletes the commitment without a refund. The tokens stay in escrow. Recovery: open.
- **IBC client.** The update from [IBC](#ibc) works the same, and later transfers work both ways.
- **Warp transfer from Celestia.** It stays minted on the EVM chain without collateral on Celestia. The next message reuses its nonce. A relayer with a fresh database delivers it and later messages (see [Hyperlane](#hyperlane)).

Not shown by the tests: a relayer that keeps its old database (from the relayer code, it skips the reused nonce and can stall), and Hyperlane validators.

### Planned restart

A planned restart moves IBC clients to a new revision of the chain-id, for example `celestia-1`. The e2e test `TestIBCPlannedUpgradeRestart` (not merged yet, #8666) runs this flow; its [nightly run](https://github.com/celestiaorg/celestia-app/actions/runs/44079709600) with a plain export at U-1 passed.

1. Choose the upgrade height U and I = U + 11. No block above U can be committed, so an honest validator signs at most U + 1, below I. Use this I in the proposal and in the export.
2. Pass one governance proposal with three messages: `MsgResetCircuitBreaker` for `MsgIBCSoftwareUpgrade`, `MsgIBCSoftwareUpgrade` with U and the upgraded client state (the new chain-id, latest height (1, I)), and `MsgTripCircuitBreaker`. The circuit breaker blocks `MsgIBCSoftwareUpgrade` by default.
   The proposal must execute by the EndBlock of block U-2. The IBC module stores the upgraded consensus state only in the BeginBlock of U-1, and only if the upgrade is already scheduled. A proposal that executes in the EndBlock of U-1 still halts the chain at U, and the export still succeeds, but Hermes has no upgraded consensus state to prove. A proposal executes in the EndBlock of the first block whose time is at or after its `voting_end_time`. So choose U so that block U-2 comes well after that time, at least an hour of blocks at the current block time, and check that `celestia-appd query upgrade plan` shows U before the chain reaches U-2.
3. The chain halts at U. Block U is committed but never executed, so its transactions are always dropped. Not every validator exits at U: in a nightly run of the test, 1 of 3 exited and the others waited one height below. Once one validator has exited, stop all of them.
4. Before item 7 of [Before the restart](#before-the-restart), set up two archives of the old chain. `export.sh` reads header U from `--rpc`, and Hermes needs block U later:
   - copy `data/` from a validator that exited at U, without `data/cs.wal` and keeping each archive's own `priv_validator_state.json`. Gossip may not deliver block U;
   - peer the archives only with each other: `persistent_peers` set to the other archive, `seeds = ""`, `pex = false`. Hermes refuses a node that is catching up, and a node without peers stays catching up;
   - start them with `--unsafe-skip-upgrades=<U>`, so they execute block U.
5. Before item 13 of [Before the restart](#before-the-restart), check on an archive that the upgraded client state and the upgraded consensus state for U exist at height U-1. Hermes proves both at that height. Each line must print `present`:

   ```sh
   for k in upgradedClient upgradedConsState; do
     key=$(printf 'upgradedIBCState/%s/%s' "$U" "$k" | od -An -v -tx1 | tr -d ' \n')
     curl -s "$ARCHIVE_RPC/abci_query?path=%22/store/upgrade/key%22&data=0x$key&height=$((U - 1))" |
       jq -r --arg k "$k" '"\($k): \(if .result.response.value then "present" else "missing" end)"'
   done
   ```

6. E is U-1 and C is U. The IBC upgrade trusts the next validators hash in header U-1, the validator set of block U. An export at U-1 (not merged yet, #8665) checks that its validator set matches that hash, which holds when block U-1 changed no validator power. Otherwise it fails, and `export.sh` shows `upgrade "<name>" is scheduled for height <U> and block <U-1> changed the validator set: export with --height <U-2> instead`. Then run item 7 of [Before the restart](#before-the-restart) again with `--height <U-2>` and an empty `--out`: the transactions of block U-1 are dropped too, and [Export below the tip](#export-below-the-tip) applies to them. The test covers only the export at U-1.
7. Pass the new chain-id to `export.sh` with `--chain-id`.
8. After the restart, wait until an archive serves the commit of U, its app height is U and it is not catching up. Relayers point the Celestia entry in Hermes at that archive and move each client with `hermes upgrade client --host-chain <counterparty-chain-id> --client <client-id> --upgrade-height <U>`. Then they point the entry back at a node of the new chain, rename it to the new chain-id as in [New chain-id without a proposal](#new-chain-id-without-a-proposal), and start Hermes. In the test, a transfer sent before the halt was delivered once and transfers worked both ways at revision 1.

## Known limitations

- `genesis from-export`, its checks and the export at the last height before an upgrade ship from app v12. They work only on a chain already on v12; there is no v11 backport.
- If validators with 1/3 or more of voting power can't take part, the new chain does not produce blocks. There is no tool to replace the validator set.
- The dry start runs only InitChain. State that imports but fails in the first BeginBlock or EndBlock is not caught.
- The import resets the staking unbonding ID counter to 1 and loses the unbonding-type and reverse ID indexes, as measured on a mainnet export. Queued unbondings still pay out. A fix in the cosmos-sdk fork is tracked.
- The first block after the restart sends the fee collector balance to the community pool, because distribution has no previous commit to pay. Supply does not change; only the recipients do.

More than 2/3 of voting power reproduce the export SHA-256, so a tampered export is outside this procedure. The genesis tool does not check these malformed inputs, which an honest export does not produce. Each one stops InitChain:

- an ordinary account at a module account address;
- invalid addresses in distribution, slashing, authz, feegrant, circuit or redelegation records;
- an authz grant without an authorization;
- a non-active validator with a malformed consensus key or a power above int64;
- a negative missed-block index;
- two fee allowances for one pair, or a blocked grantee without an account;
- capability port ownership that conflicts with transfer or interchain accounts;
- a Hyperlane ISM of an unsupported type, or a malformed validator storage address;
- duplicate evidence;
- an empty packet-forward key, a fibre payment hash that is not 32 bytes, or a key or value above the store limits.

An account number so large that the import runs for an impractically long time is not checked either.

## Open questions

- An IBC client whose only usable trusted height is C (no stored consensus height below C within the trusting period): not tested. Options are a patched Hermes that builds the header from the archive's validators at C+1, or governance recovery once the client expires; ibc-go v8 rejects `MsgRecoverClient` while the client is Active. The e2e tests cover only a client below E; a client at C is a follow-up case.
- Hyperlane relayer after an export below the tip: the fresh-database procedure above is checked only with a noop ISM and no Hyperlane validators.
- Celestia to EVM messages dispatched at or below E but not delivered before the halt are never seen by a fresh relayer database: their events are in old blocks. They are valid on the new chain. Options, none checked: find them in the old database and deliver them separately, run a relayer once against an archive up to E, or let the old relayer drain during the halt (it also delivers messages above E).
- Hyperlane validators that see a reused nonce likely detect a reorg and stop, and then need a database reset. Not checked.
