# Recovery drills on a talis network

A drill runs one recovery tool end to end on a test network: rollback, a patched binary, or a new chain from an export. It checks the scripts and runbooks against real nodes and records how long each step takes. One person plays all validators, so a drill measures the technical part, not the time to get validators with more than 2/3 of voting power to act.

Last run: 2026-11-18, release at commit `9bb0fd009`, four validators. The results are in [Results](#results).

## Network

Follow `tools/talis/README.md`. Four validators with equal power: two of them hold 1/2, more than 1/3, so they halt the chain, and less than 2/3, so they can't continue it alone.

```sh
talis init -c hf-drill-1 -e hf-drill -r <repo> -s ~/.ssh/id_rsa.pub -n <ssh-key-name>   # chain-id becomes talis-hf-drill-1
talis add -t validator -c 4 --slug s-4vcpu-8gb -r fra1
talis up --workers 20
talis genesis -b <repo>/build -a <repo>/build/celestia-appd
talis deploy --direct-payload-upload --workers 20 -s ~/.ssh/id_rsa
talis txsim -i 2 -s 2 -m 1000 -x 20000 --fire-and-forget -k ~/.ssh/id_rsa
```

- Keep txsim blobs small. The default 1-1.9 MB blobs build blocks of about 120 MB, and `s-4vcpu-8gb` nodes run out of memory.
- talis nodes run without a tx index and discard ABCI responses, so `celestia-appd query tx` finds nothing. Check a transaction through the block that holds it.
- Tear the network down with `talis down` when done, and check in DigitalOcean that no droplet of the experiment is left.

`tools/talis/hardfork-drill.sh` runs commands over ssh on the validators: stop and start nodes, swap binaries, run `recovery.sh` and `export.sh`, and record a timeline. Run it without arguments for the list of commands. Set `EXP` to the talis experiment directory.

## Drill builds

A drill build is the release plus one file that adds entries to `stateFixes` in `app/state_fixes.go` for the drill chain-id and a height. The file has the build tag `drill` and stays out of the repository. Set the heights and the mode with `-ldflags -X`, and set `version.Version` too, or `--expect-version` has nothing to check. Build static binaries the way `make build-talis-bins` does. The modes used:

- `mismatch`: at height B, send 1 utia to the fee collector. Run on half of the power, it splits the app hash and halts the chain at B+1.
- `badstate`: at B, send 1,000,000 utia to the gov module, a bad write all nodes agree on. At A above B, return an error, which halts the chain.
- `fixed`: the same write at B, and at A burn the 1,000,000 utia from gov instead of the error.

A 200 MB binary uploads slowly from a home connection. Upload it once and copy it to the other validators with `hardfork-drill.sh fanout` over the private network. Choose B far enough ahead for the upload and the swaps: at about one block per second, 20 minutes is 1,200 blocks.

## Rollback

1. Swap validators 0 and 1 to `mismatch` before B, and start them. Keep 2 and 3 on the release.
2. At B+1 the chain halts: validators 2 and 3 log `wrong Block.Header.AppHash`. Start the clock.
3. On validator 0: put the release back, `recovery rollback --height <B-1>`, start. The chain continues on 3 of 4.
4. On validator 1, some minutes later: the same. It catches up.
5. `recovery all verify --check-height <B+1> --expect-app-hash <hash>`. `verify --check-height H` reads the app hash in header H, the state after H-1.

## Patched binary

1. Swap all validators to `badstate` before B, one at a time.
2. At A every node logs `CONSENSUS FAILURE` and exits. Watch the processes with `hardfork-drill.sh procs`, not only the heights: an exited node and a stuck node look the same in `heights`.
3. Swap all validators to `fixed`, run `recovery restart`, start them.
4. Check that the gov balance is 1,000,000 utia from B to A-1 and 0 from A, and that all nodes have the same app hash after A.

## New chain from an export

Follow [export-coordination.md](export-coordination.md) and [export.md](export.md). Validator 2 stands in for an archive node: it stays on the old chain as the `--rpc` of `export.sh` until the exports are done.

1. Stop validators 0 and 1. Read C from validators 2 and 3, take E = C, and compute I from the signer heights.
2. Export on a stopped validator. `export.sh` brings it to E from validators 2 and 3 if it stopped below E; in the drill validator 0 stopped at E-1 and reached E. `export 0 --height E --rpc <validator 2> --out /root/hf-export --genesis-time G --initial-height I --chain-id <chain-id>`. If `app_hash_at_height` is `not verified`, compare the `finalized block` line for E in `node.log` with `/abci_info` of validators 2 and 3.
3. Stop validator 3, then reproduce the export on validators 1 and 3 and compare `export.json.sha256`. `export.sh` refuses a node that runs.
4. Dry start the candidate in a scratch home on a stopped validator.
5. Stop validator 2. Copy the genesis to all validators, run `recovery new-chain ... --peers <list> --dry-run` on each.
6. Run `new-chain` with `--peers` on validators 0, 1 and 2 and start them; interrupt one run and run it again. After G, run `new-chain` with `--peers` on validator 3 and start it.
7. After G: the first block is I, no evidence, a new blob is in a block, `verify --chain-id` passes on all nodes, and every validator signs.

Choose G 10 to 15 minutes ahead on a small network. On mainnet G must leave time to download the genesis and run the dry runs.

## Results

2026-11-18, four `s-4vcpu-8gb` validators in fra1:

| Drill | Halt to blocks | Notes |
| --- | --- | --- |
| Rollback | 3 min 13 s | The late validator rolled back 7 minutes later and caught up. Same app hash, no evidence. |
| Patched binary | 2 min after the fix was built | The fix was built before the drill, so writing and reviewing it is not measured. |
| New chain | 44 min 41 s | Export and genesis took 9 s, three validators reproduced the export hash. 26 minutes were spent waiting for G, chosen 45 minutes ahead, and 4 minutes on missing `--peers`. |

Found and fixed: the talis genesis had gov params that fail validation, so `genesis from-export` refused the export; `new-chain` without `--peers` left talis nodes with no peers; the drill helper's `stop` left the node running when it outlived its tmux session.
