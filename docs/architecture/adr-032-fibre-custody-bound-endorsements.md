# ADR 032: Fibre Custody-Bound Endorsements and Signer Payments

## Changelog

- 2026-11-03: Initial draft

## Status

Proposed

## Summary

Today a validator earns the same Fibre fees whether or not it runs a Fibre server, and its signature on a payment promise does not show that it received any data. This ADR fixes both inside the protocol, so the 2.2 GiB/s scale-up does not rest on validators provisioning hardware out of goodwill, or on probes they can recognise and game.

Four changes ship together:

1. **Pay signers.** A PFF's payment goes to the validators whose signatures land on chain within `K` blocks of the promise height, pro rata by stake, instead of to `fee_collector`.
2. **Sign a custody root.** The validator's signature also covers a salted Merkle root over the rows assigned to it, so a valid signature needs the row bytes.
3. **Challenge custody.** Each block, the block hash picks random (endorsement, row) pairs that the signer must open on chain. A wrong opening is slashed. A missed one jails the validator and charges a fault fee.
4. **Let validators endorse late** (recommended). Validators can add their own endorsements to a settled promise with `MsgEndorse`, and the chain verifies every signature, not only the quorum prefix. Clients stop deciding who gets paid.

Together these turn every real upload into a measurement. A validator's credited bytes divided by its expected bytes is the throughput it delivered at the load the network actually carried, and that number decides its pay. Bandwidth is never slashed.

## Context

### Validators are not paid for Fibre service

`MsgPayForFibre` deducts the blob charge from escrow and sends it to `fee_collector` (`x/fibre/keeper/msg_server.go:348`). `x/distribution` then splits it across the bonded set by voting power (`AllocateTokens`). A validator that never runs a Fibre server earns the same Fibre fees as one that carries its full share. Under-provisioning costs reputation at most, and only if someone measures it.

### A signature does not prove the validator received anything

Validators sign the promise's sign bytes: escrow signer key, namespace, upload size, commitment, blob version, height and creation time (`strippedSignBytes`, `fibre/payment_promise.go:212`; signed by `SignPaymentPromiseValidator`, `:292`). None of these depend on the rows. The reference server verifies and stores the shard before it signs (`fibre/server_upload.go:112`), but that is an implementation choice the protocol cannot check. A modified server can sign as soon as it parses the promise.

Two consequences follow. The availability argument assumes signers hold their rows, and nothing enforces that. And any metric built on signatures (who signed, how fast) rewards a server that signs without receiving, because it is always the fastest. Paying signers without fixing this would pay validators to lie. Decisions 2 and 3 exist to make Decision 1 safe.

### What each validator must carry

The mainnet target is 2.2 GiB/s of uploads. Reads come on top of that at twice the rate, about 4.4 GiB/s. A validator with stake fraction `s` is assigned `min(1, 3s)` of every blob's original rows (`Set.AssignedRows`, `fibre/validator/set.go:108`, liveness threshold 1/3), subject to the `MinRowsPerValidator` floor. Its upload ingress is that fraction of the write rate, plus row proofs: 14 sibling hashes of 32 bytes per row, about 1.4% overhead at 128 MiB blobs and 5.5% at 32 MiB.

| Stake | Share of rows | Ingress at 2.2 GiB/s    |
|-------|---------------|-------------------------|
| 0.5%  | 1.5%          | 34 MiB/s (283 Mbit/s)   |
| 1%    | 3%            | 68 MiB/s (567 Mbit/s)   |
| 2%    | 6%            | 135 MiB/s (1.1 Gbit/s)  |
| 5%    | 15%           | 338 MiB/s (2.8 Gbit/s)  |
| 10%   | 30%           | 676 MiB/s (5.7 Gbit/s)  |
| ≥ 1/3 | 100%          | 2.2 GiB/s (18.9 Gbit/s) |

These are sustained averages, and peaks are higher. Across the whole set, validators ingest about 3 × 2.2 ≈ 6.6 GiB/s. Reads add egress of roughly `s × 4.4 GiB/s` on average, about 45 MiB/s (378 Mbit/s) for a 1% validator. Large validators serve more than that, because readers try them first (`Set.Select`). Storage requirements follow ADR-029.

At the current charge of 0.004 utia per gas, uploads pay about 0.76 TIA per GiB. At 2.2 GiB/s that is roughly 144,000 TIA a day. This ADR redirects that flow, and it grows with demand.

### Why outside probes are not enough

The obvious alternative is a team-run prober that periodically sends authenticated test uploads to each validator. A validator can recognise a probe by its key, account, IP or timing. It can then serve probes first, or scale up only while probed, and still fail real users. Even a probe it cannot recognise only measures signatures, which, as shown above, prove nothing. The full comparison is under [Alternatives considered](#alternatives-considered).

## Decision

### 1. Pay signers within `K` blocks

- At settlement, `PayForFibre` moves the charge into an `x/fibre` pool keyed by promise hash instead of sending it to `fee_collector`.
- A validator is **credited** for a promise if a valid endorsement from it (in the Decision 2 format) is on chain at a height ≤ `promise.height + K`. The endorsement can come in the PFF itself or in a `MsgEndorse` (Decision 4).
- When the window closes, the BeginBlocker (`x/fibre/keeper/abci.go:9`) takes the community tax and splits the rest among credited validators, pro rata by voting power at `promise.height`. It pays them through `AllocateTokensToValidator`, so commission and delegator rewards work as they do today. A validator that wasn't credited gets nothing for that blob, and its share goes to the validators that did the work. If the PFF itself lands after `promise.height + K`, nobody is credited and the charge goes to `fee_collector` as it does today.
- Every non-empty signature position must verify. Today `validateValidatorSignatures` returns as soon as quorum is reached (`msg_server.go:413`), and CheckTx/ProcessProposal only verify the `QuorumPrefix` (`x/fibre/keeper/preverify.go:283`, `fibre/validator/signature_set.go:50`). FinalizeBlock does not re-verify. Under this ADR an unverified position later in the list would still be paid, so ProcessProposal must reject any PFF that contains an invalid signature.

`K` is what makes bandwidth measurable. A validator that falls more than `K` blocks behind loses the payment for every blob in its backlog, so it can't hide a backlog or get paid for buffered work. A validator's delivered throughput at real load is its credited bytes over its expected bytes for each epoch, computed from chain state alone. Blocks are about 2.6 s (`DelayedPrecommitTimeout` + `TimeoutCommit` in `pkg/appconsts/app_consts.go`).

Credit is split by stake rather than by assigned rows. Assignment is already proportional to stake except at the min-rows floor and the 1/3 cap, and distribution already works by stake. See open question 5.

### 2. Custody-bound endorsements

The validator signs a new version of the sign bytes:

```text
SignBytes_v1 = RawBytesMessageSignBytes(chain_id, "fibre/pp:v1", stripped_v0 || custody_root)

salt         = SHA256("fibre/custody:v1" || promise_hash || validator_address)
custody_root = RFC 6962 Merkle root over leaves (salt || u32be(j) || row[j])
               for the validator's assigned rows j, in Assign order
```

- The server computes the root after `verifyShard` and before signing (`fibre/server_upload.go:112`). This adds one SHA-256 pass over bytes it has just hashed to check the row proofs.
- `MsgPayForFibre` and `MsgEndorse` carry one `custody_root` per signature position, and the chain rebuilds `SignBytes_v1` from it.
- Clients hold every row, so they can check roots and drop bad endorsements early. Safety does not depend on this, because the challenges in Decision 3 catch bad roots.

**Why the salt is needed.** The rsema1d row tree hashes each leaf as `SHA256(0x00 || row)` (`pkg/rsema1d/SPEC.md`, Merkle section), and each row's proof contains its sibling's leaf hash. The commitment shuffles rows across validators (`Set.Assign`, `fibre/validator/set.go:65`), so one validator's leaf hashes sit inside other validators' proofs. `DownloadShard` serves those proofs to anyone, and peers can forward a leaf hash in 32 bytes instead of a 32 KiB row. A root over unsalted row hashes could therefore be built without ever holding the rows. Salting with the promise hash and validator address makes every leaf unique to one (promise, validator) pair, so the only way to compute it is to hash the row bytes.

**What the root proves.** Whoever computed it had the bytes of the validator's rows at signing time. It does not prove those bytes sit on the validator's own hardware. A validator may delegate its Fibre server to a third party, and that server has to receive and keep the data like any other.

### 3. Custody challenges

- Each block, the BeginBlocker derives `seed = SHA256("fibre/challenge:v1" || last_block_hash)` and selects `C` challenges. Each challenge is a credited endorsement still inside its retention window, plus one row position in that validator's assignment.
- Within `N` blocks, the challenged validator submits `MsgCustodyResponse` from any account. It carries the row, its rsema1d row proof, `rlcOrigRoot` and the custody path. The chain checks `SHA256(rowRoot || rlcOrigRoot) == commitment` (`SPEC.md` §3.5) and that the salted leaf opens against the signed `custody_root`. At the 128 MiB maximum blob size (`fibre/protocol_params.go:68`) a response is about 33 KiB.
- Outcomes:
  - **Valid opening:** nothing happens.
  - **Wrong opening:** the path verifies against the signed root, but the row is not in the commitment. The consensus key has signed a root over data that is not the blob. That is objective evidence, so the validator is slashed, as for double signing.
  - **No valid opening within `N` blocks:** the validator is jailed and pays a fault fee. A miss can be an outage, so it is not treated as fraud.
- At 2.2 GiB/s and 128 MiB blobs there are about 45 PFFs per block, so up to 4,500 endorsements. With `C = 1`, each validator is challenged about every 100 blocks (about 4 minutes). A server that signs without data cannot open anything, so it is jailed at its first challenge.

A cheater facing a challenge will choose not to answer rather than answer wrongly, so the miss path is the real deterrent. The fault fee must exceed what a lazy signer collects between challenges (open question 6).

### 4. Late endorsements (recommended)

- `MsgEndorse { promise_hash, entries: [{validator_index, signature, custody_root}] }` adds endorsements to a promise a PFF has already settled, provided it lands by `promise.height + K`. Anyone can submit it, and validators batch their own entries once per block.
- The client still returns at 2/3 (`specs/src/fibre_client.md`), so users wait no longer than today. A slower validator that finishes in time is still paid. The client no longer decides who gets paid, so leaving a validator out of the PFF, or selling a place in it, changes nothing.
- One gap remains: a client that never sends a validator its rows. The validator can rebuild them from peers through `DownloadShard` and still endorse, at the cost of fetching the blob. A client that withholds rows systematically shows up on chain.
- An endorsement is about 100 bytes. If the slowest third of validators endorse late, that is about 1,500 entries (~150 KB) per block with 128 MiB blobs, and four times that with 32 MiB blobs. Verifying every signature means about 4,500 ed25519 checks per block, which is about 300 ms on one core and must be batched or parallelised; with 32 MiB blobs it is about 18,000 checks.

## Enforcement

| Evidence | Objective? | Consequence |
|----------|------------|-------------|
| A custody opening whose row is not in the commitment | Yes, the consensus key signed it | Slash |
| Two different custody roots for one promise | Yes, the root is deterministic | Slash |
| No valid opening within `N` blocks | Attributable, but may be an outage | Jail and fault fee |
| Endorsement not on chain within `K` blocks | No: client, network and proposer all affect it | Lose that blob's payment |
| Low ratio of credited to expected bytes over an epoch | No | Lower rewards only |

The rule is: **never slash for bandwidth.** A slow upload can always be blamed on the client, so bandwidth only decides who gets paid. Slashing and jailing are reserved for custody, where the evidence is a signed statement the chain can check.

## Alternatives considered

- **Authenticated sampling endpoint.** Each validator hardcodes a team public key, only the team can send probe traffic, unauthenticated requests are rejected cheaply, and failures feed social pressure. It is simple and free for validators, but probes are identifiable by design. A validator can serve probes first, scale up only when probed, or rent capacity for the test. It measures an endpoint rather than real service, and the only enforcement is social. It remains useful for onboarding checks.
- **Gateway fill-to-target load.** Push paid filler blobs through the fibre-gateway encoders, using the same escrow accounts, IPs and sizes, so probes look like customer traffic and load every validator at once. It needs no protocol change and makes a good readiness test. But it trusts the gateway operator, costs about 0.76 TIA per GiB plus encoder egress, only measures while it runs, and under today's rules still gives full marks to servers that sign without data. Under this ADR it becomes a stronger test, because only credited, custody-bound endorsements count.
- **Protocol-scheduled probe windows.** The chain opens random windows and pays for probe blobs that top load up to the target. This tests spare capacity above organic demand. It is deferred because it needs a funded set of probers. Since Decision 1 pays for every blob equally, probes would not need to be hidden: a validator that serves them first loses credit on real traffic.
- **Peer probing with on-chain reports.** Validators probe each other and post scores. It is decentralised, but prober identities are public, a prober's uplink caps the test, failures cannot be attributed, and cartels can trade good reports. It is noisy and could only weight rewards.
- **Client receipts.** Clients sign per-validator latency receipts. Clients can lie and validators can self-deal, so this is dashboard material only.
- **Unsalted shard hash in the signature.** Fails for the reason given under Decision 2.

## Consequences

### Positive

- Fibre revenue follows service. At the upload target that is about 144,000 TIA a day, paid only to validators that took their rows in time.
- A signature becomes evidence. A validator that signs without data is jailed within minutes, and one that signs a false root is slashed.
- Each validator's delivered throughput is an on-chain number, so anyone can build a public capacity dashboard from chain data without running probes. Under-provisioned validators become visible to delegators and the community, not just paid less.
- With Decision 4, clients cannot steer payments or grief a validator by leaving it out.
- Self-dealing does not pay. A validator that uploads its own blobs pays the full charge and gets back only its stake share.

### Negative

- It breaks consensus. It adds a new sign domain, new messages (`MsgEndorse`, `MsgCustodyResponse`, slashing evidence), state for pending payments and challenges, and new parameters `K`, `N`, `C` and the fault fee. Servers and clients must upgrade together.
- It adds chain load: full signature verification, endorsements and ~33 KiB challenge responses.
- Validators far from the main clients sign later and will lose more blobs. `K` must be generous enough that distance alone does not cost a validator its share.
- The incentive is only as large as demand. At today's volume, fee routing barely matters, though custody challenges bite at any volume.

### Residual risks

- **Colluding uploader.** The salt is public, so an uploader that colludes with a validator can compute that validator's root and send it no rows. The validator then fetches only the rows it is challenged on, and this design cannot detect it. A secret salt (open question 4) closes this gap.
- **Retention.** A validator that received its rows and later deleted them can rebuild a challenged row from peers within `N` blocks. Challenges prove receipt strongly but retention only weakly.
- **Outsourcing.** A custody proof shows possession by someone acting for the validator, not by the validator's own machine. This is intended, since a validator may delegate its Fibre server to a third party.

## Open questions

1. **Values of `K`, `N` and `C`.** Start with `K` ≈ 8 blocks (~21 s, above the client's 15 s `RPCTimeout`), `N` at 20–50 blocks and `C` = 1. Tune them on a testnet at target load.
2. **Who receives the fee on `MsgPaymentPromiseTimeout`** (`msg_server.go:222`). Timeout settles without signatures, so nobody is credited. The options are `fee_collector` (today's behaviour), burning it, or allowing `MsgEndorse` before settlement so validators that stored the shard are paid. Leaning towards `fee_collector`, since a timed-out promise usually means the upload failed.
3. **BLS aggregation.** A PFF carries about 100 ed25519 signatures, plus late endorsements. A BLS key registered in `x/valaddr` would shrink that to one aggregate signature and a bitmap, at the cost of a second validator key and pairing checks. At target load it is likely worth it, and it becomes necessary if 32 MiB blobs dominate (about 18,000 ed25519 checks per block).
4. **Secret salt, as in Ethereum's proof of custody.** Derive the salt from a secret only the validator knows, reveal it in challenge responses, and make early disclosure slashable. Then no uploader can compute a validator's root for it. The cost is a second signer call per upload and more evidence logic.
5. **Credit by stake or by assigned rows.** Small validators at the min-rows floor carry more than their stake share.
6. **Fault fee and jail length.** The fee must exceed what a lazy signer collects between challenges: about 4 TIA for a 1% validator over 100 blocks at target. Jail length should mirror downtime jailing.
7. **Rollout and upgrade path.**
   1. Ship everything in one app version. Promises whose `height` is before the upgrade settle under v0 rules and pay `fee_collector`. Servers sign v1 from the upgrade height, and clients accept both versions during the transition.
   2. Start in shadow mode for a period set by governance: compute credit and run challenges, emit events, but keep paying `fee_collector` and do not jail. Operators see their scores before any money moves.
   3. Turn on payments, then jailing, by parameter. Run on Mocha at target load before mainnet.

## References

- `fibre/payment_promise.go:189,212,292`: sign-bytes domain, stripped fields, validator signing.
- `fibre/server_upload.go:112`: where the server signs.
- `fibre/validator/set.go:65,108`: `Assign` and `AssignedRows`.
- `fibre/validator/signature_set.go:50`: `QuorumPrefix`.
- `x/fibre/keeper/msg_server.go:133,222,348,385,413`: `PayForFibre`, `PaymentPromiseTimeout`, the transfer to `fee_collector`, signature validation and its early return.
- `x/fibre/keeper/preverify.go:283`: prefix-only verification in CheckTx and ProcessProposal.
- `x/fibre/keeper/abci.go:9`: BeginBlocker.
- `fibre/protocol_params.go:58`: default protocol parameters (4096 rows, 1/3 liveness, 128 MiB maximum blob).
- `pkg/rsema1d/SPEC.md` §3.5 and the Merkle section: commitment and leaf hashing.
- `pkg/appconsts/app_consts.go`: block timing.
- cosmos-sdk `x/distribution/keeper/allocation.go`: `AllocateTokens`, `AllocateTokensToValidator`.
- `specs/src/fibre.md`, `specs/src/fibre_server.md`, `specs/src/fibre_module.md`, `x/fibre/README.md`.
- ADR-029: stake-derived storage budgets.
- Ethereum proof of custody (consensus-specs custody game); FlashFlow (Tor relay bandwidth measurement).
