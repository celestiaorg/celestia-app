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

A trust root at genesis would remove the weak-subjectivity input: the genesis
validator set (built by InitChain from the gentxs in `genesis.json`), whose hash
must equal `ValidatorsHash` of header 1, followed by `fshdr forward` from
height 1. Deriving that validator set from the gentxs is not implemented; see
[Headers](#headers) for that and for the plan to ship verified headers next
to the changesets.

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

`fsboot` takes these from its sources as the stock command does, so a single
source is trusted for them. It requires at least two distinct `-rpc` servers
that return identical values (one honest server detects a forgery) and checks
them against header `H`'s proposer and the local application DB; with
`-src-data` they come from a node you run.

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
make build   # binaries in ./build (fastindex46 is its own module, built by the same target)
make test
```

The tools land in a series of changes; this table lists the full set.

| command | purpose |
|---|---|
| `fshdr verify` | verify headers down from one trusted header (hash chain) and write `expected.tsv` |
| `fshdr forward` | verify headers up from one trusted header (links, commits, validator sets) and write `expected.tsv` |
| `fshdr headers` | print the rows of a block store without verification, for comparisons |
| `fsboot bootstrap` | write the CometBFT state and block store at `H` from verified data |
| `fastindex`, `fastindex46` | build the IAVL fast-node index of an applied DB offline (SDK v0.50 / v0.46 stores) |
| `bsfix` | delete blocks above state height + 1 after an unclean stop |
| `scripts/fastsync.sh` | run steps 1 to 3 (and checkpoints) end to end |
| `scripts/segment.sh` | run and check one re-execution segment |

### fshdr

Both modes write the same `expected.tsv`: row `h` holds, from header `h+1`, the
app hash after `h`, the header hash, `LastResultsHash`, `ValidatorsHash`,
`NextValidatorsHash` and the app version.

`verify` walks down from an anchor. The anchor is either verified by a light
client from a trust root inside the trusting period, or supplied by the
operator. Headers come from RPC (`/blockchain`) or a block store and only need
to hash-link, so this is fast (SHA-256 per header):

```sh
# light-client anchor at to+1, headers over RPC
fshdr verify -chain-id celestia -rpc https://rpc-a.example,https://rpc-b.example \
  -trust-height 16000000 -trust-hash <HEX> -from 1 -to 1000000 -out expected.tsv

# operator anchor (hash of header 1000001 from sources you trust), headers from a local block store
fshdr verify -chain-id celestia -blockstore ~/.celestia-app/data -backend pebbledb \
  -anchor-height 1000001 -anchor-hash <HEX> -from 1 -to 1000000 -out expected.tsv
```

`forward` walks up from a trusted header and checks each height as a full node
does: the hash link, the validator-set transition, that the previous commit is
the canonical one, and that +2/3 of the validator set's power signed. It needs
commits and validator sets but no trusting period, so it can run from an old
checkpoint to the tip:

```sh
fshdr forward -chain-id celestia -data ~/.celestia-app/data -backend pebbledb \
  -trusted-height 6680339 -trusted-hash <HEX> -to 6700000 -out expected.tsv
```

One header type covers every mainnet height: the header and commit encoding
and hashing are identical in the celestia-core v0.34 and v0.38-based lines
(`internal/hdrchain`, mainnet fixtures in its `testdata`). A future encoding
change adds a `HeaderType` there.

### fsboot

`fsboot bootstrap` writes the CometBFT state store and block store of a node
home at height `H`: the bytes `celestia-appd comet bootstrap-state --height H`
writes, from data checked against the verified hash of header `H+1`. It needs no
light client and no trusting period, so it works at any height `expected.tsv`
covers (checkpoints and old targets included). The node home must have empty
CometBFT stores, as for the stock command.

```sh
fsboot bootstrap -home ~/.celestia-app -height 1000000 -expected expected.tsv \
  -rpc https://rpc-a.example,https://rpc-b.example
# or take headers, commit, validator sets and params from a stopped node you run
fsboot bootstrap -home ~/.celestia-app -height 1000000 -expected expected.tsv -src-data /srv/archive/data
```

`-dry-run` runs every check and writes nothing.

Proposer priorities and the consensus params other than block max bytes and
max gas are in no header hash, so the source that provides them is trusted for
them, as in the stock command. fsboot therefore requires at least two distinct
`-rpc` servers, which must return identical values (`-allow-single-source`
accepts one), and it checks them where it can: the priorities must name header
`H`'s proposer, the params' app version must equal header `H+1`'s, and the
params must equal those the local application DB stored at `H` (x/consensus,
or the x/params `baseapp` subspace for app v1 to v3) when that DB is present.
With `-src-data` the source is a node you run, which you trust for them. The
tests run the stock `BootstrapStateWithGenProvider` and `fsboot` against the
same RPC servers and require identical stores, and cover forged validator
sets, priorities, consensus params, commit signatures, headers and trusted
hashes.

With celestia-core v0.43 the offline state sync height is kept until the node
stores its first block, so a first start that fails before that can simply be
retried.

### fastindex

The appliers write with the IAVL fast-node index off. A node with the default
`iavl-disable-fastnode = false` then rebuilds the index of every store inside
its first `LoadVersion`, before it does anything else, which takes minutes or
more on mainnet-size state and logs nothing at `log_level = warn`. `fastindex`
does that rebuild offline, visibly, and checks that the commit id did not change
(the index is not part of any hash):

```sh
fastindex   -db ~/.celestia-app/data -backend pebbledb -check   # exit 3: a node would rebuild
fastindex   -db ~/.celestia-app/data -backend pebbledb          # app v4+ DB (SDK v0.50, IAVL v1)
fastindex46 -db ~/.celestia-app/data -backend pebbledb          # app v1-v3 DB (SDK v0.46, IAVL v0.19)
```

The cost is one pass over the leaves of the final state; history does not
matter. `fastindex46` is a separate module (`fastindex46/`) because it needs
the SDK v0.46 store that celestia-app v3 uses.

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
