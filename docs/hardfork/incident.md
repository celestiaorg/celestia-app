# Respond to a chain halt

This runbook is for the protocol team. Open it first when blocks may have stopped. It tells whether the chain really halted, how to assess the damage and which recovery tool to use.

The tools have their own runbooks:

- Rollback: [rollback-coordination.md](rollback-coordination.md).
- In-place surgery: [surgery-coordination.md](surgery-coordination.md).
- New chain from an export: [export-coordination.md](export-coordination.md).

## Steps

1. Confirm that the chain halted: [Is it a halt?](#is-it-a-halt)
2. Send the halt notice and contain the bridges: [Contain in the first minutes](#contain-in-the-first-minutes).
3. Assess the damage: [Assess the damage](#assess-the-damage).
4. Choose the tool and follow its runbook: [Choose the tool](#choose-the-tool).
5. Stop the clock when the recovery check passes: [The 24h clock](#the-24h-clock).

## Roles and channels

Fill in the placeholders before an incident.

- Incident lead: `<incident lead>`, backup `<backup incident lead>`. Leads the incident and decides which tool to use.
- Internal channel: `<incident channel>`.
- Validators: `<validator channel>`, and `<validator contact list>` to reach every validator in the active set directly.
- Bridges: `<Hyperlane operator contacts>`, `<warp route owners>`, `<IBC relayer operators>`, `<counterparty chain contacts>`.
- Others: `<RPC providers and exchanges>`, `<fibre clients>`, `<celestia-node team contact>`.
- Public status: `<status page>`.

## Is it a halt?

Blocks normally come every few seconds. One dead RPC, one stuck node or a monitoring gap is not a halt.

1. Read the latest height and block time on several independent RPC endpoints, ours (`<our RPC endpoints>`) and public ones (`<public RPC endpoints>`). Run it twice, a minute apart:

   ```sh
   for RPC in <rpc-1> <rpc-2> <rpc-3>; do
     r=$(curl -fsS --max-time 5 "$RPC/status") || { echo "$RPC: no answer"; continue; }
     echo "$r" | jq -r --arg rpc "$RPC" '"\($rpc) \(.result.sync_info.latest_block_height) \(.result.sync_info.latest_block_time)"'
   done
   ```

2. If the height grows on any endpoint, there is no network halt. If committed state is wrong on the running chain, follow [surgery-coordination.md](surgery-coordination.md). If the problem is confined to one node:
   - Our validator is down: the chain goes on and we miss signatures. Fix the node before it is jailed for downtime.
   - One node falls behind and logs `wrong Block.Header.AppHash`. Compare its app hash in `/abci_info` with the app hash in the header of the next height on an independent node, as in item 2 of [Before the restart](rollback-coordination.md#before-the-restart). If they differ, roll back that node with [rollback.md](rollback.md).
3. If no endpoint shows a new height, read the consensus state on two or three nodes. Run it twice:

   ```sh
   s=$(curl -s --max-time 5 "$RPC/consensus_state")
   echo "$s" | jq -r '.result.round_state."height/round/step"'
   echo "$s" | jq -r '.result.round_state | (."height/round/step" | split("/")[1] | tonumber) as $r | .height_vote_set[] | select(.round == $r) | .prevotes[]' | awk '{print ($1 == "nil-Vote" ? "missing" : $3)}' | sort | uniq -c
   ```

   The first line is height/round/step. The rest counts the validators per block hash they prevoted in the current round: `000000000000` is a vote for nil, `missing` is no vote received, and several other hashes are votes for competing blocks. A halt looks like this: the height stays and the round keeps growing. Do not wait for low vote shares: nil votes count as votes, so the shares in `prevotes_bit_array` can be above 2/3 while nothing commits. `/dump_consensus_state` also shows what each peer reports.
4. Check the validator logs, ours and those validators share:

   ```sh
   journalctl -u <service> --since "-30 min" | grep -E 'CONSENSUS FAILURE|wrong Block.Header.AppHash'
   ```

   - `CONSENSUS FAILURE!!!`: consensus on the node stopped on an error, usually in block execution. The round no longer changes, and `/consensus_state` may not answer.
   - `wrong Block.Header.AppHash`: the node disagrees with the app hash in a block header. A node logs it also when it rejects a proposal, so it does not prove that a wrong header was committed. Record the height, then compare `/block` and `/commit` at that height on independent nodes.
5. No new height on any endpoint for several minutes, together with growing rounds or a log sign from item 4, is a halt. Record the time of the first alert in `<incident channel>` and send the halt notice.

## Contain in the first minutes

1. Send the halt notice to validators ([templates](#message-templates)). It asks them not to delete data or reset signer state.
2. Ask the Hyperlane validator and relayer operators to stop their Celestia agents now and keep their databases. Their agents may have signed checkpoints for state that a rollback or a fix removes. Before they resume, confirm their deployed `reorgPeriod` with them (the public Hyperlane registry lists 1 for Celestia) and reconcile the checkpoints with the recovered chain.
3. If funds may have been minted or stolen, do not wait for the full assessment:
   - Ask the warp route owners to pause their routes on the EVM side.
   - Ask the IBC relayer operators to stop relaying packets from Celestia, and the counterparty chains to hold.

## Assess the damage

Do this before choosing a tool; it is the same for all three. Post the answers in `<incident channel>`.

1. Heights. Ask every validator for its block height, app height and app hash, with the commands in item 2 of [Before the restart](rollback-coordination.md#before-the-restart). Record:
   - FIRST: the first height with bad state.
   - X: the last height whose state is correct. It is the rollback target, and the export height E for a new chain.
   - C: the last committed height.
   - Whether the network committed a header with a wrong app hash. The app hash of block N is in the header of block N+1. Check the committed headers on independent nodes; a log line alone is not enough.
2. What is wrong: the module, the accounts, whether funds were minted or lost, and how much. If the cause is unknown, collect evidence with [app-hash-mismatch.md](../maintainers/app-hash-mismatch.md).
3. What left through bridges. List the IBC and Hyperlane events from FIRST to C with the loop in item 4 of [Before the restart](surgery-coordination.md#before-the-restart), using `seq "$FIRST" "$C"` as its range: F is not chosen yet. For each one, ask the relayer operators whether it was delivered on the other chain.
4. What other chains saw. On each counterparty chain, read the latest height of its Celestia client. A height above FIRST means that chain trusts an app hash of the bad state:

   ```sh
   <counterparty binary> query ibc client state <client-id> --node <counterparty rpc>
   ```

   Ask the Hyperlane validator operators for the last checkpoint they signed: its index and height.
5. Which validators have correct state. Compare the app hashes from item 1 and sum the voting power on each side. Use the validator set for C+1, the next height that needs a commit:

   ```sh
   curl -s "$RPC/validators?height=$((C + 1))&per_page=100" | jq -r '.result.validators[] | "\(.address) \(.voting_power)"'
   ```

   Note which nodes executed the blocks and which were restored from a snapshot or state sync. A restored node may not have the state at X.
6. Time left for IBC. A client expires at the timestamp of its latest consensus state plus its trusting period. Compare that with the expected restart time. Do not rely on the `Active` status: it uses the host chain's block time, and Celestia's block time stops during the halt.

   ```sh
   <binary> query ibc client state <client-id> --node <rpc>
   <binary> query ibc client consensus-state <client-id> --latest-height --node <rpc>
   ```

   Run this with `celestia-appd` for Celestia's own clients, and on each counterparty for its Celestia client. See also [Damage by halt length](#damage-by-halt-length).

The result is the tool and whether bridges must stay paused.

## Choose the tool

The times are estimates, not measured; the drills will measure them. In every tool the slow part is getting validators with more than 2/3 of voting power to act.

| Tool | Use when | Do not use when | Halt to blocks (estimate) |
| --- | --- | --- | --- |
| Rollback | No committed header has a wrong app hash, and X is in the current app version | A committed header has a wrong app hash; X is before an upgrade | 3h best, 5-14h usual |
| In-place surgery | Committed state is wrong, and a state fix at height F can repair it | The chain fails before the fix runs: in an upgrade migration, consensus params, `PrepareProposal` or `ProcessProposal`; the fix needs a new store, message or proto type | 6h best, 8-24h usual; needs the fix code and a `[high]` review |
| New chain from an export | The state can't be repaired on the same chain | The export or InitChain fails; 1/3 or more of voting power is lost | 1-3 days to blocks, a week or more for the ecosystem |

The full rules are in [When to use a rollback](rollback-coordination.md#when-to-use-a-rollback), [When to use surgery](surgery-coordination.md#when-to-use-surgery) and [When to use a new chain](export-coordination.md#when-to-use-a-new-chain).

### When to move to the next tool

- From rollback to surgery or a new chain: the network committed a header with a wrong app hash, or X is before an app version upgrade, so the rollback command refuses.
- Pruned nodes are not a reason to change the tool. Restore state at or below X from an archive node or a snapshot, then replay.
- From surgery to a patch: the chain fails before the fix runs, in an upgrade migration, while loading consensus params, or in `PrepareProposal` or `ProcessProposal`. Patch and test that code path. Consider a restart from an export only if the patch is not feasible.
- Validators with 1/3 or more of voting power are lost or will not act, so the old set can't commit a block: no tool fixes this today. A new chain keeps the exported validator set, and there is no tool to replace it.

## Cases that need separate coordination

- Damage already left through bridges: containment, then governance on the counterparty or compensation.
- No node has correct state: start from retained state at a good height (an archive or a snapshot). Replay to state that matches the committed headers, or export that state for a reviewed restart.
- The network split on the app hash, and no side is clearly correct: investigate first, then roll back or repair the minority.
- The new chain can't start, because the export or InitChain fails: diagnose and patch that path. Surgery helps only if the chain can reach the fix height.
- Keys of more than 2/3 of voting power are compromised: no tool fixes this today, because a new chain keeps the exported validator set. Damage on other chains can't be reversed.

## Bridges that stay paused

A bridge may stay paused after the chain recovers. Each one comes back only through its owner. Announce every paused bridge, with its owner and the condition to resume.

- Hyperlane agents for Celestia: `<Hyperlane operator contacts>` restart them after they reconcile the checkpoints with the recovered chain.
- Warp routes paused on the EVM side: `<warp route owners>` unpause them after the losses are counted or compensated.
- IBC relaying: `<IBC relayer operators>` resume after they update the clients.
- Counterparty clients that froze or expired: governance of that chain recovers them with `MsgRecoverClient`.

## The 24h clock

The goal is to recover within 24 hours. The clock starts at the first alert: the page or report that started the incident. It stops when all of these hold:

1. Blocks are produced on the agreed chain. Every committed block has precommits from more than 2/3 of voting power.
2. The app hash matches on several independent nodes. Choose H, a height committed after the restart; after surgery, H must be above the fix height F, so its header holds an app hash computed after the fix. Read HASH from the header of H on a node that you checked follows the recovered chain on the right release:

   ```sh
   curl -s "<rpc>/block?height=$H" | jq -r .result.block.header.app_hash
   ```

   Then run this against several independent nodes:

   ```sh
   bash recovery.sh verify --check-height "$H" --expect-app-hash "$HASH" --rpc <rpc>
   ```

3. A new blob is included in a block. Submit one, then query its transaction on an RPC node with a tx index. It must print a height and code `0`:

   ```sh
   celestia-appd tx blob pay-for-blob 0x00010203040506070809 0x48656c6c6f --from <key> --chain-id <chain-id> --fees <fee> --node <rpc> --yes
   celestia-appd query tx <txhash> --node <rpc> -o json | jq -r '.height, .code'
   ```

4. Every bridge has resumed, or the incident lead has announced that it stays paused (see [Bridges that stay paused](#bridges-that-stay-paused)).

## Appendix

### Damage by halt length

Default params are shown; check the current ones.

- Over 10 minutes: IBC packets sent with the transfer CLI's default 10-minute timeout can be timed out. Their refunds need relayers to relay the timeouts. An ICA packet that times out closes its ORDERED channel.
- Over 1 hour: fibre uploads whose `MsgPayForFibre` did not land are never committed, and clients upload them again (`PaymentPromiseTimeout`, 1 hour). See [Fibre after a halt](rollback-coordination.md#fibre-after-a-halt).
- Over 4 hours: fibre servers may prune the shards of pre-halt uploads (`ShardRetention`, 4 hours). Their promises can still be charged with `MsgPaymentPromiseTimeout`, but nothing sends it automatically.
- Over 24 hours: pre-halt payment promises can no longer be settled (`WithdrawalDelay`, 24 hours). Governance proposals whose voting period ended during the halt are tallied right after the restart, with no time to vote.
- Over about 9 days: counterparty clients of Celestia expire. Their trusting period is usually 2/3 of the 14-day unbonding time; check each client.

### Message templates

Halt notice, to validators:

```text
Celestia <chain-id> halted at height <height>, <time UTC>.
Do not delete node data. Do not reset signer state (priv_validator_state.json or your remote signer).
Reply in <validator channel> with your block height, app height and app hash.
Next update by <time UTC>.
```

Bridge hold, to Hyperlane and IBC operators:

```text
Celestia <chain-id> halted at height <height>, <time UTC>.
Hyperlane: stop your Celestia validator and relayer agents now and keep their databases.
IBC relayers: stop relaying packets from Celestia until we confirm the recovery.
We will tell you when to resume in <channel>.
```

Tool chosen, to validators:

```text
Recovery plan for <chain-id>: <rollback to height X | surgery at height F | new chain>.
Release: <version>, <release link>. recovery.sh tag and checksum: <tag>, <sha256>.
Who acts: <all validators | list>. Follow <runbook link> with these values.
Post your dry run result in <validator channel> by <time UTC>.
Restart time: <time UTC>.
```

Restart, to validators:

```text
It is <time UTC>. Run the recovery step of <runbook link> now.
Post your moniker in <validator channel> when your node is up.
```

Recovered, to everyone:

```text
<chain-id> recovered. Blocks resumed at height <height>, <time UTC>.
App hash at height <H>: <HASH>. Check it with: bash recovery.sh verify --check-height <H> --expect-app-hash <HASH>
Validators: check that you sign (<runbook link>, step <8 in rollback.md and surgery.md, 12 in export.md>). If you are jailed: celestia-appd tx slashing unjail --from <key>
Bridges: <resumed | paused until <condition>, owner <owner>>.
```
