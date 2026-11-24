# Header archive for fastsync

This is a design plan. `fshdr` and `internal/hdrchain` implement the verification it relies on;
the packed header files, the checkpoint skip chain and the genesis validator-set derivation are
not implemented yet.

Goal: every header (and enough commits and validator sets) from genesis to the tip, so a node
replaying the changeset bundle can check each height's post-state app hash (`AppHash` of header
h+1) without trusting whoever served the bundle.

## 1. One header format for all heights

- App v1-v3 (heights 1..6,680,339) ran celestia-core v1.55.0-tm-v0.34.35. From v4 (6,680,340)
  the core is v0.38-based (v1.57.2-tm-v0.38.17, v1.59.0-tm-v0.38.17, v0.42.8; main uses v0.43.x).
- Header, Commit, CommitSig, CanonicalVote and SimpleValidator are byte-identical in both
  lines, and so are the hash functions. `BlockProtocol` stays 11. Vote extensions are disabled,
  so `EC:` records and `ExtendedCommit` never matter.
- So one codec (`hdrchain.CometBFT`) decodes and verifies every height. This was cross-checked
  on real headers from both eras with both libraries: equal hashes, equal proto bytes, equal sign
  bytes, and both accept the same commits. The fixtures in `internal/hdrchain/testdata` verify
  with the current types.
- The multiplexer runs a single celestia-core for every height, so a node synced from genesis
  writes blockstore.db and state.db for the v0.34 era in today's format (same `H:`/`C:`/`SC:`
  keys).
- Mainnet first heights per app version: v2 2,371,495; v3 2,993,220; v4 6,680,340; v5 6,748,822;
  v6 8,662,013; v8 10,960,600 (v7 skipped); v9 11,771,699; v10 15,116,659 (tip about 16.19M in November 2026). Found by binary search over `/header` on an archive RPC.

## 2. Where the data comes from

| Source | Covers | Notes |
|---|---|---|
| Our own node, synced from genesis with the multiplexer | 1..tip | best: `blockstore.db` (`H:` meta, `C:` canonical commit) and `state.db` (`validatorsKey:`). Read offline with `hdrchain.BlockstoreSource` (`fshdr verify -blockstore`, `fshdr forward -data`). Needs `min-retain-blocks=0` (no pruning). |
| A public archive RPC (lowest height 1) | 1..tip | `/commit?height=h` gives header + canonical commit; `/validators?height=h&per_page=100`. |
| publicnode, polkachu RPCs | about the last 0.6M heights only (pruned) | useful as witnesses near the tip, not for history. |
| Other archive providers (Numia, Lava, QuickNode, Grove, Celenium/indexers) | unknown | each one is just another untrusted source; check before relying on it. |
| Changeset bundle | none | has no headers; the header file is a separate artifact. |

Since everything is verified, a source only needs to be available and correct. A wrong
source fails verification and never gets accepted. Use several in rotation (`HTTPSource` does this).

## 3. Sizes and bandwidth (16.2M heights)

Measured on the fixtures (proto): header 324-403 B; commit 6.1 KB (75 validators) to 7.5-9.6 KB
(100 validators; a CommitSig with its 64 B signature is about 95 B); full `ValidatorSet` proto
about 7.3 KB (`SimpleValidator` hash leaves alone are about 100 x 41 B = 4 KB).

- Headers only: 16.2M x 0.4 KB = about 6.5 GB raw. Packed (drop `LastBlockID.Hash`, which is
  the previous header's hash and gets recomputed; dictionary for the chain ID, proposer,
  validators/consensus/evidence hashes; zstd), about 200-250 B/height, so **3-4 GB**.
- Commits for every height: about 16.2M x 8 KB = **about 130 GB**, mostly incompressible
  signatures. Too big to ship with every bundle.
- Validator-set changes: sampling 240 random heights found `ValidatorsHash != NextValidatorsHash`
  in about 43% of v1-v2 heights, about 11% of v3 heights, about 1.4% of v4-v6 heights and about 0%
  after that. That is about 1.8M changes (±30%). Most change a few voting powers, so diffs
  cost about 20-40 B each, **about 50-100 MB**, plus one full set per 100k heights.
- Fetching from RPC: about 2 requests per height for headers and commits, plus validators on
  changes, so about 35M requests. At 8 in parallel and 50 ms each that is about 2.5 days per
  provider. Our own blockstore reads the same in hours, so it is the primary source.

## 4. Trust model (trust-minimised, from genesis forward)

1. **Root**: `genesis.json` (chain ID, initial height 1). The genesis validator set is not in
   `genesis.validators` (it is empty). InitChain builds it from the gentxs (MsgCreateValidator
   pubkey and self-bond; power = tokens / 10^6). A small offline tool derives it and checks that its
   hash equals `ValidatorsHash` of header 1 (`83387942B3A5...`, 75 validators). Checking the
   result against two independent RPCs' `/validators?height=1` is an extra check, not the root.
2. **Sequential verification** (`hdrchain.Verifier`, `RunForward`): at each height h, check the
   link (`LastBlockID.Hash = hash(h-1)`), `ValidatorsHash(h) = NextValidatorsHash(h-1)`,
   `hash(commit h-1) = LastCommitHash(h)`, and that +2/3 of vals(h) power signed commit h.
   Validators are fetched only when the hash changes. This is the strongest check, but it needs
   every commit (about 130 GB) and about 1.2B ed25519 verifications (about 70 per height; that is
   about 8-17 core-hours with the batch verifier, under an hour on 16 cores; I/O dominates). The bundle **publisher** runs it once
   against our own blockstore and records the result: a signed manifest entry with the tip
   hash and a digest of the header file.
3. **What a bundle consumer does** (cheap, still trustless):
   - Anchor: verify one recent header with the light client from a weak-subjectivity trust root,
     as fshdr does today, or use the checkpoint skip chain (below) from
     genesis.
   - Hash chain: `VerifyBackward` from the anchor down to height 1 over the shipped headers.
     That is 16M SHA-256 merkle hashes, about 1-2 minutes. Collision resistance then
     authenticates every header, and so every AppHash, under the anchor.
   - Checkpoint commits (optional): shipping a commit at every validator-set change would be
     about 1.8M x 8 KB = 14 GB, which is too much. Instead, ship the canonical commit and the full
     validator set every 10k heights and at each upgrade height: about 1.7k checkpoints, about
     25 MB. Each checkpoint is a light-client skip step from the previous one: >1/3 of the
     previous trusted set and +2/3 of the new set signed. If that fails, fall back to bisection
     over intermediate checkpoints fetched from any RPC. The validator sets come from the
     valsets file, checked against the hash-chained headers. This gives a from-genesis check without a trusting-period anchor,
     under the usual long-range assumption (old keys did not collude). `hdrchain` does not
     implement the skip step yet: use `types.VerifyCommitLightTrusting`.
4. Every source stays untrusted. Bounds and failure handling: response size cap, validators
   at most 10000, page/total consistency, store panics turned into errors.

## 5. Packaging next to the changeset bundle

```text
bundle/
  manifest.json            # existing changeset manifest, plus a "headers" section
  changesets/...           # existing
  headers/
    headers-000000001-001000000.bin.zst   # packed headers, 1M heights per segment
    ...
    valsets-*.bin.zst      # full set at segment start + per-change diffs
    commits-checkpoints.bin.zst            # optional, see 4.3
```

- Segments are aligned with the changeset segments, so a consumer that resumes at height H
  downloads only the segments at or above H. Each record is length-prefixed proto
  (`tendermint.types.Header`, `Commit`, `SimpleValidator` diffs) with a 1-byte codec id, so a
  future header format just gets a new id (`hdrchain.ForAppVersion`).
- The manifest `headers` section lists per segment `{from, to, sha256, bytes,
  hash_of_last_header, last_block_id_of_first_header}` plus `codec: "cometbft-block11"`,
  `chain_id` and the publisher's anchor `{height, hash}`. Segment boundary hashes let segments be
  verified in parallel, and they join into one chain once the boundaries match.
- The `fshdr` output (`expected.tsv`: h, AppHash(h+1), hash(h+1), ...) is derived from the header
  file. It does not replace the header file, so a consumer can always recompute it.
- Publishing: append new segments as the tip grows. Old segments never change (immutable,
  content-addressed by sha256).
