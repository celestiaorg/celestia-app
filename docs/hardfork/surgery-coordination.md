# Coordinate in-place surgery

This runbook is for the protocol team. Validators and node operators follow [surgery.md](surgery.md).

In-place surgery repairs bad state on the same chain. A release lists a state fix in `app/state_fixes.go`. The fix runs once, at fix height F, before BeginBlock, on every node that executes F. Nodes that sync from genesis later run it too.

## When to use surgery

Use surgery when the state is wrong and the network has already committed it, so a rollback can't fix it:

- `FinalizeBlock` fails at height X on every node because of bad state, and the chain halted. F is X. Block X is saved, and on restart the node executes it again with the fix.
- The state is wrong, but the chain still produces blocks, for example after an exploit. F is a future height that leaves time to release and install the fix.

Do not use surgery when:

- The network did not commit the wrong app hash. Use a rollback: [rollback-coordination.md](rollback-coordination.md).
- The chain fails before the fix runs: in an upgrade migration, while it loads consensus params, or in `PrepareProposal` or `ProcessProposal`. These need a patch of that code.
- The fix needs a new store, a new message or a new proto type. That needs an app version upgrade.

## Write the fix

Add an entry to `stateFixes` in `app/state_fixes.go`:

```go
var stateFixes = []StateFix{
	{
		ChainID: "celestia",
		Height:  F,
		Name:    "short-name",
		Apply: func(ctx sdk.Context, app *App) error {
			// repair the state through the keepers
			return nil
		},
	},
}
```

Review the fix as consensus-critical code:

- `Apply` must be deterministic: no map iteration, wall-clock time, randomness, floats or node config. See [INV-1](../ai/invariants.md).
- An error from `Apply` stops every node at F. Return an error only when the state is not what the fix expects.
- Do not change consensus params.
- Do not touch IBC clients, connections, channels, packet commitments or sequences.
- Do not touch the Hyperlane mailbox, its merkle tree hook or nonces. Hyperlane validators have signed checkpoints over them.
- Do not touch fibre escrow accounts or payment promises without the fibre team. Fibre servers hold promises signed against the old balances.
- A fix can't undo effects on other chains, such as IBC transfers or warp mints.
- `PrepareProposal` and `ProcessProposal` of block F still see the state before the fix.
- Add a test like `TestStateFixRepairsHaltedChain` in `app/state_fixes_test.go`.
- The PR title has `!` and `[high]`, and the PR gets an adversarial review.

## Release the fix

- The fix goes into a patch of the release line that executes F. That is the line of the app version at F (`app_version` in `/abci_info`). For example, a fix at an app v10 height goes into a v10.x patch.
- Every later patch of that line keeps the fix.
- A later major release runs F in its embedded binary of that line. Its pin must be a tag with the fix. A copy of the fix in the later major's source never runs. Update the pin in all of these together:
  - `CELESTIA_V<N>_VERSION` in `Makefile`;
  - `v<N>Version` in `internal/embedding/data.go`;
  - `CELESTIA_VERSION_V<N>` in `docker/multiplexer.Dockerfile`;
  - the `v<N>` lines in `.goreleaser.yaml`;
  - the `v<N>` checksums in `scripts/embedded_checksums.txt`.
- If validators run a later major release, release it again with the new pin. Check every release binary and image with `celestia-appd passthrough v<N> version`.
- A new chain from a zero-height export with the same chain ID would run the fix again at F. Give that chain a new chain ID, or drop the fix from its release.

## Before the restart

1. Announce the incident in the validator channels. Ask validators not to delete data or reset signer state.
2. Ask each validator for its block height, app height and app hash, as in item 2 of [rollback-coordination.md](rollback-coordination.md#before-the-restart).
3. Check that no node executed F on the old release. On a halted chain, the app height is F-1 on every node. A node with an app height of F or more rolls back to F-1 first: [rollback.md](rollback.md). If the network already committed block F+1 without the fix, the fix can't run at F: choose a new F.
4. Find what the bad state sent to other chains. Its app hashes are in committed headers, so IBC relayers and Hyperlane validators may already have used them. On a node with `discard_abci_responses = false`, list the IBC and Hyperlane events from FIRST, the first height with bad state, to F-1:

   ```sh
   for N in $(seq "$FIRST" $((F - 1))); do
     r=$(curl -fsS "localhost:26657/block_results?height=$N") || { echo "$N: query failed"; continue; }
     echo "$r" | jq -r --arg n "$N" 'if .result == null then "\($n): no block results" else ([.result.txs_results[]?.events[]?, .result.finalize_block_events[]?] | map(.type | select(startswith("hyperlane.") or IN("send_packet", "recv_packet", "write_acknowledgement", "acknowledge_packet", "timeout_packet"))) | unique[] | "\($n): \(.)") end'
   done
   ```

   If the bug did not touch IBC or Hyperlane state, these events are valid and need nothing. If it did, other chains may have accepted transfers or messages from the bad state. The fix can only compensate on Celestia; it can't undo what other chains accepted. Before the restart, tell the counterparty chains, the Hyperlane operators and the warp route owners.
5. Announce F, the release and the restart time. On a chain that still produces blocks, validators with more than 2/3 of voting power must run the release before the chain reaches F.
6. Ask every validator to run the dry run (steps 1-5 of [surgery.md](surgery.md#steps)). Continue when every report prints the release version and no `error:` line.
7. On a halted chain, confirm before the restart time that validators with more than 2/3 of voting power are ready.
8. At the restart time, validators run the restart.

## Confirm the chain recovered

1. Check that blocks are produced, as in item 1 of [Confirm the chain recovered](rollback-coordination.md#confirm-the-chain-recovered).
2. Check that the fix ran: validator logs show `applied state fix` with the fix name and F. Query the repaired state and check its value.
3. Choose H above F, post H and HASH, and check several nodes, as in items 2 and 3 of [Confirm the chain recovered](rollback-coordination.md#confirm-the-chain-recovered).
4. Ask validators to run step 8 of [surgery.md](surgery.md#steps) to check that they sign.

## After the restart

A halted chain has the same halt effects as a rollback. Work through items 1, 2, 4, 7, 8 and 9 of [After the restart](rollback-coordination.md#after-the-restart): IBC, interchain accounts, fibre, governance, jailed validators and explorers.

Then make sure the fix stays in every later release, as in [Release the fix](#release-the-fix).
