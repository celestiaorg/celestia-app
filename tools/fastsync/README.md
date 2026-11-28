# fastsync

Tools to bring a celestia-app node from genesis to a recent height much faster
than replaying every block, while still checking the result against the chain
that +2/3 of the validators committed.

This is operator tooling. It lives in its own Go module and is not part of the
celestia-appd binary or of any consensus code path.

## The hybrid

Replaying the chain from genesis executes every block: roughly 16M blocks at
tens of milliseconds each. Most of that time goes into work whose result is
already pinned by the block headers: each header `h+1` carries the app hash of
the state after block `h`.

The hybrid splits genesis sync into a fast pass and an optional parallel check:

1. **Verify headers.** `fshdr` anchors one header with a light client (or an
   operator-supplied hash) and checks the hash chain from it down to the first
   height: the hash of each header must equal `LastBlockID.Hash` of the header
   above it. Every header it accepts is the one the committed chain contains,
   so its `AppHash` is a trusted value. The output is `expected.tsv`: one row per
   height `h` with the app hash after `h` and the hash of header `h+1`.
2. **Apply changesets.** An applier writes the recorded store changes
   (changesets) of each block into the application database and, after each
   height, compares the resulting app hash with `expected.tsv`. A mismatch stops
   the run. The appliers (one per store generation: SDK v0.46 for app v1 to v3,
   SDK v0.50 for app v4 and later) are added in a later change.
3. **Bootstrap CometBFT.** `fsboot` writes the CometBFT state store and block
   store at the target height `H`, the same bytes that `celestia-appd comet
   bootstrap-state` writes, from data it checks against the verified header
   `H+1`. The node then starts at `H`; its handshake checks that the
   application's height and app hash equal the state at `H`, and block sync
   continues from `H+1` with full execution.
4. **Re-execute segments (optional, for full verification).** While applying,
   the run can stop at checkpoint heights and save a bootstrapped node home for
   each. Each segment `[c_k, c_k+1]` then runs a normal node from checkpoint
   `c_k` with `--halt-height c_k+1 + 1`, which executes every block of the
   segment. The segment passes when the node halts cleanly and its own block
   store holds the verified headers at the end of the segment. Segments are
   independent and run in parallel on separate machines.

After step 3 the node holds a state whose app hash at every height equals the
committed one. After step 4 every state transition has also been executed by
stock code.

## Trust model

What the operator trusts:

- **One trust root**: either a light-client trust height and hash (the same
  weak-subjectivity input as state sync, which must be inside the trusting
  period), or the hash of one header supplied out of band (`-anchor-hash`). Get
  it from sources you trust, for example several independent explorers or your
  own node.
- **The binaries** built from this repository and celestia-appd.

What is verified, and against what:

| item | check |
|---|---|
| headers `from+1..anchor` | hash chain down from the anchor; chain id; `ValidateBasic` |
| application DB at every applied height | app hash equals `AppHash` of the verified header `h+1` |
| header `H+1` used by `fsboot` | hash equals row `H` of `expected.tsv` |
| header `H` | hash equals `header(H+1).LastBlockID.Hash` |
| validator sets `H`, `H+1`, `H+2` | hash equals `ValidatorsHash` / `NextValidatorsHash` of the verified headers |
| commit for `H` | block id equals `header(H+1).LastBlockID`; signed by +2/3 of validator set `H` |
| consensus params for `H+1` | hash equals `header(H+1).ConsensusHash` |
| segment re-execution | stock node executes every block; boundary headers equal the verified ones |

What no header authenticates (the same as in `comet bootstrap-state`):

- validator proposer priorities (not part of the validator-set hash);
- consensus params other than block max bytes and max gas
  (`ConsensusParams.Hash` covers only those two).

`fsboot` takes these from its source as the stock command does. With several
`-rpc` servers it also requires all of them to return identical values, so one
honest server is enough to detect a forgery. With `-src-data` they come from a
node you run.

Without step 4, the application state is only known to have the committed app
hash at each height; the transitions between heights were not executed locally.
Collision resistance of the app hash makes this the committed state, but bugs in
execution code that +2/3 of validators share would not be caught. Step 4
executes them.

Data sources are never trusted: RPC servers, block stores and changeset files
can be forged, and every value taken from them is checked as listed above.

## Commands

Build and test (the module is outside the repository's `go.work`):

```sh
cd tools/fastsync
make build   # binaries in ./build
make test
```

The tools land in a series of changes; this table lists the full set.

| command | purpose |
|---|---|
| `fshdr verify` | verify headers and write `expected.tsv` |
| `fsboot bootstrap` | write the CometBFT state and block store at `H` from verified data |
| `fsboot inspect` | print the store heights and whether the node can start |
| `fastindex` | build the IAVL fast-node index of an applied DB offline |
| `bsfix` | delete blocks above state height + 1 after an unclean stop |
| `scripts/fastsync.sh` | run steps 1 to 3 (and checkpoints) end to end |
| `scripts/segment.sh` | run and check one re-execution segment |

### bsfix

A node whose block sync saved a block ahead of execution can, after an unclean
stop, hold blocks above state height + 1 and refuse to start. With the node
stopped:

```sh
bsfix -data ~/.celestia-app/data -backend pebbledb -dry-run
bsfix -data ~/.celestia-app/data -backend pebbledb
```

`bsfix` deletes only blocks above state height + 1. It does not touch the state
store or the application DB.

## Headers

One header type (`internal/hdrchain`) covers every mainnet height: the
header, commit, vote and validator-set encodings and hashes are identical in
the celestia-core v0.34 line (app v1 to v3) and the v0.38-based line (app v4
and later). A node synced from genesis with the multiplexer stores all heights
in the current block store format, so its `blockstore.db` and `state.db` are
the best header source; any archive RPC also works. Every source is untrusted.

Plan, not implemented yet: ship verified headers next to the changesets, so a
consumer needs no archive node.

- Packed headers (about 3-4 GB for 16M heights) in segments aligned with the
  changeset segments, with boundary hashes in the manifest so segments verify
  in parallel. Commits for every height (about 130 GB) are too large to ship;
  instead, a checkpoint commit and validator set every 10k heights and at each
  upgrade (about 25 MB) gives a light-client skip chain from genesis.
- A from-genesis trust root: the genesis validator set, which InitChain builds
  from the gentxs in `genesis.json` (its hash must equal `ValidatorsHash` of
  header 1). `hdrchain.NewGenesisVerifier` takes that hash.

## Status

The tools were validated on a local devnet and on mainnet ranges. They are
experimental: run them on machines you can rebuild, keep the `expected.tsv`
and logs of every run, and compare the node's app hashes with an independent
node after it starts.
