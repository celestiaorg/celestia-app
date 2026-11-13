# ADR 032: Fibre Capacity Incentives

## Changelog

- 2026-11-03: Initial draft
- 2026-11-12: Rewritten around the problem statement. Signer payments become phase 1 and stand alone. Custody proofs, late endorsements and penalties become optional phase 2 extensions. Load numbers now include the row floor.

## Status

Proposed

## Summary

Fibre's throughput is set by the weakest validator in the best 2/3 of stake. Today the protocol cannot see how much capacity any validator has, and a validator's income does not depend on it. Capacity is a private cost with a shared reward, so the rules reward letting other validators carry the load.

This ADR proposes a fix in two phases:

- **Phase 1 (proposed for implementation).** Clients include every signature they collect, the chain verifies all of them, and each PayForFibre (PFF) fee is paid only to the validators that signed, in proportion to the rows they carried. The share of validators that did not sign goes to the community pool. This makes a validator's Fibre income depend on its own work and puts a per-validator participation record on chain.
- **Phase 2 (optional extensions).** Late endorsements, a client rebate, signatures that prove custody of the data, and penalties. Each closes one gap that phase 1 leaves open, and each can be adopted separately when data from phase 1 shows it is needed.

## Context

### How Fibre works today

1. A blob is encoded into 16,384 rows. Any 4,096 distinct rows rebuild it.
2. Each validator is assigned rows in proportion to its stake: `ceil(4096 × 3 × stake share)`, capped at 4,096, with a floor of 148 rows (`Set.AssignedRows`, `fibre/validator/set.go:108`; `MinRowsPerValidator`, `fibre/protocol_params.go:136`). The floor gives each validator enough samples to check the encoding with 100 bits of security.
3. The client sends each validator its rows. A validator that received and verified them signs the payment promise. The upload is done when signatures reach 2/3 of stake. There is no requirement on the number of validators.
4. The client submits a PFF carrying the signatures. The chain verifies them only until 2/3 of stake is reached (`QuorumPrefix`, `fibre/validator/signature_set.go:50`).
5. The PFF charge goes to `fee_collector` (`x/fibre/keeper/msg_server.go:348`), and `x/distribution` splits it across the bonded set by stake.

### Throughput depends on the best 2/3 of stake

An upload completes as soon as validators holding 2/3 of stake have signed. Network throughput is therefore the capacity of the slowest validator in the fastest set that reaches 2/3. The remaining 1/3 adds no throughput. It is the reserve: it replaces a validator in the fast set that falls behind, and it adds read redundancy.

Availability does not depend on this ADR. Any 1/3 of stake holds enough rows to rebuild a blob, whatever the stake distribution.

### Capacity is a private cost with a shared reward

Carrying Fibre traffic costs a validator bandwidth, storage and CPU, and the cost grows with throughput. Fibre fees are split by stake among all validators. A validator that provisions for the target earns exactly as much as one that provisions for nothing, so the cheapest strategy for each validator is to let the others carry the load. No validator has to be dishonest for this to happen.

The mainnet target is 2.2 GiB/s of uploads, with reads on top at about twice that rate. The table shows upload ingress per validator at the target, for the mainnet validator set on 2026-11-12 (98 bonded validators). Row proofs add 1.4% at 128 MiB blobs and 5.5% at 32 MiB.

| Stake              | Validators | Rows per blob | Ingress at 2.2 GiB/s    |
|--------------------|------------|---------------|-------------------------|
| Below 1.2% (floor) | 78         | 148           | 81 MiB/s (683 Mbit/s)   |
| 2%                 |            | 246           | 135 MiB/s (1.1 Gbit/s)  |
| 5%                 |            | 615           | 338 MiB/s (2.8 Gbit/s)  |
| 12% (the largest)  | 1          | 1,473         | 810 MiB/s (6.8 Gbit/s)  |
| ≥ 1/3              | 0          | 4,096         | 2.2 GiB/s (18.9 Gbit/s) |

The floor means every validator below 1.2% of stake carries the same load, 3.6% of every blob. Across the whole set, 19,046 rows are assigned per blob, so validators ingest about 4.65 × 2.2 ≈ 10.2 GiB/s in total.

The floor also makes the mismatch between work and pay worst for small validators:

| Group               | Share of stake and fees | Share of rows carried | Needed for the 2/3 quorum? |
|---------------------|-------------------------|-----------------------|----------------------------|
| Top 26 validators   | 67%                     | 44%                   | Yes                        |
| Other 72 validators | 33%                     | 56%                   | No                         |

The 72 validators that the quorum never strictly needs carry more than half of the data for a third of the fees. A validator with 0.1% of stake carries about 12 times its stake share. These validators have the weakest reason to provision.

At the current charge of 0.004 utia per gas, uploads pay about 0.76 TIA per GiB, roughly 144,000 TIA a day at the target rate.

### Nobody can see who keeps up

- **The chain records only the first 2/3.** The reference client collects signatures until quorum (`fibre/client_upload.go:191`) and drops the ones that arrive later. Nothing records which validators were skipped or slow.
- **Registration shows that a server exists, not what it can carry.**
- **A signature does not prove the data was stored.** The signed bytes cover the commitment, size and height, but not the rows. The reference server verifies and stores the shard before signing (`fibre/server_upload.go:112`), but a modified server can sign without keeping anything.
- **Retention and serving are invisible.** Nothing shows how long a validator keeps data or whether it serves reads.

### Under-provisioning shows up too late

Slow validators are skipped silently, so the network looks healthy while the reserve erodes. The first visible symptom is uploads stalling for everyone, once more than 1/3 of stake cannot keep up. Real traffic also tests capacity only up to today's load, never the headroom for tomorrow's.

### Consensus has a feedback loop and Fibre does not

|                                            | Consensus                         | Fibre today                             |
|--------------------------------------------|-----------------------------------|-----------------------------------------|
| Is each validator's work recorded?         | Yes, every block lists who signed | No                                      |
| What if a validator does not do the work?  | It is jailed and stops earning    | Nothing                                 |
| What does skipping the work save?          | Almost nothing                    | Most of the cost: bandwidth and storage |

Consensus can rely on honest behaviour because cutting corners saves little and is recorded. In Fibre, cutting corners saves the dominant cost and is not recorded.

### Requirements

| Requirement                                                          | Level        | Met by  |
|----------------------------------------------------------------------|--------------|---------|
| **Measure:** a public per-validator record of who kept up            | Must         | Phase 1 |
| **Motivate:** a validator's income depends on that record            | Must         | Phase 1 |
| **Stay fair:** nobody profits from another validator being skipped   | Must         | Phase 1 |
| **Stay cheap:** no measurable cost to block production               | Must         | Phase 1 |
| **Warn early:** show missing capacity before real load hits it       | Nice to have | Phase 1 |
| **Protect skipped validators:** a skipped validator loses nothing    | Nice to have | Phase 2 |
| **Prove storage:** a signature means the data is really held         | Nice to have | Phase 2 |
| **Penalise:** jailing or slashing on top of lost income              | Nice to have | Phase 2 |

## Decision: phase 1

Phase 1 is one client change and one change to fee distribution. It adds no messages and no persistent state.

### 1. Clients include every signature

After reaching 2/3 of stake, the reference client keeps collecting signatures for a short grace period, or until every validator has signed, and puts all of them into the PFF. Today it returns at quorum and discards the rest, although background uploads to the remaining validators already continue by default (`DetachBackgroundUploads`, `fibre/client_config.go:131`).

The grace period delays the PFF, not the data: the blob is already held by 2/3 of stake when it starts.

### 2. The chain verifies every signature

Every non-empty signature position in a PFF must verify. Today `validateValidatorSignatures` returns as soon as quorum is reached (`msg_server.go:385`), and CheckTx and ProcessProposal verify only the quorum prefix. Under this ADR an unverified position would be paid, so ProcessProposal must reject a PFF that contains an invalid signature.

On mainnet the quorum prefix is about 26 signatures and a full set is up to 98, so this is 3 to 4 times today's signature work. [Block production cost](#block-production-cost) shows where that work runs.

### 3. Fees are paid to signers, by rows carried

For a PFF with charge `A`, let `r_i` be the rows assigned to validator `i` in the validator set at `promise.height`, and `R` the sum of `r_i` over that set.

- The community tax is taken from `A` as it is today, leaving `A'`.
- Each validator `i` whose signature is in the PFF receives `A' × r_i / R`, paid through `AllocateTokensToValidator` so that commission and delegator rewards work as they do today.
- The shares of validators that did not sign go to the community pool. They are not redistributed to signers.
- A promise settled by `MsgPaymentPromiseTimeout` has no signatures, so its charge goes to `fee_collector` as it does today.

Three properties follow from this rule:

- **Income follows work.** A validator earns for a blob only if it signed for it, and in proportion to the rows it carried. Small validators are paid for the floor.
- **Nobody profits from a skipped validator.** A signer's payment for a blob is the same whether 67% or 100% of stake signed. A client or a validator that keeps a competitor out of a PFF gains nothing.
- **Self-dealing does not pay.** A validator that uploads its own blobs pays the full charge and gets back only its own share.

Payouts are aggregated per block. The module sums each validator's shares in memory while the block executes and calls `AllocateTokensToValidator` once per validator at the end of the block. That is at most one call per bonded validator per block, however many PFFs settled. Paying PFF by PFF would cost about 4,500 calls per block at target and must be avoided.

### 4. The record is public

Every PFF now carries the full verified set of signers, so chain data alone gives, for each validator and any period, the bytes it signed for over the bytes it was assigned. Two operational pieces use this record. Neither is part of the state machine:

- **A dashboard** showing per-validator participation and the network's reserve, meaning how much stake signs beyond 2/3. A shrinking reserve is the early warning that phase 1 provides.
- **Periodic unannounced load tests** at the target rate. Since signers are recorded and paid, a load test is ordinary traffic, and its fees pay validators for holding reserve capacity.

### What phase 1 does not solve

- **Signing without storing.** A signature still does not prove the data is held, and a modified server that signs at once is always among the fastest. Phase 1 raises the bar from doing nothing to running software that lies, and an off-chain sampler that reads rows back and checks them against the commitment can expose such a server. It cannot be proven on chain.
- **Clients that skip validators.** On mainnet, uploading only to the top 26 validators costs a client 2.0 GiB of egress per GiB uploaded instead of 4.65 GiB. Skipped validators lose their fee and their participation score through no fault of their own. Nobody else gains, but the client saves bandwidth.
- **Low traffic.** The incentive is only as large as fee volume.
- **Penalties.** Lost fees are the only consequence.

## Phase 2: optional extensions

These are not proposed for implementation now. Each can be adopted separately.

| Extension                               | Gap it closes                               | Main cost                                               |
|-----------------------------------------|---------------------------------------------|---------------------------------------------------------|
| A. Late endorsements                    | Honest validators that are slow or skipped  | New message, pending payout state, block space          |
| B. Client rebate                        | The client's reason to skip validators      | Fee logic, new parameters                               |
| C. Custody-bound signatures, challenges | Signing without storing                     | New signature format, new messages, slashing            |
| D. Penalties                            | Lost fees as the only consequence           | Risk of penalising honest validators                    |
| E. Staking rewards tied to participation | Weak incentive at low traffic              | Needs a metric clients cannot distort, so needs A and B |

### A. Late endorsements

`MsgEndorse { promise_hash, entries: [{validator_index, signature}] }` adds signatures to a promise that a PFF has already settled. Anyone can submit it, and validators batch their own entries once per block.

- The payout for a PFF is deferred until its window closes, at `max(promise.height + K, pff_height + K_e)`. Anchoring the window to the PFF height as well guarantees every validator `K_e` blocks to endorse, even when the PFF itself lands late.
- The client returns at 2/3 again, with no grace period, and no longer decides who is paid.
- Pending promises are stored as `{amount, promise.height, signer bitmap}`, indexed by the height at which the window closes.
- `MsgEndorse` must go through the same CheckTx preverification and signature cache as `MsgPayForFibre`.

A validator that never receives its rows still cannot endorse honestly. Rebuilding them from peers costs a download of the whole blob, which is not sustainable if a client skips it systematically. Extension B addresses that.

### B. Client rebate

The client is refunded a part of the charge that grows with the stake that signed beyond 2/3. Uploading to every validator then costs the client less than skipping the tail. The rebate is funded from the non-signer shares that phase 1 sends to the community pool, so signers are paid the same either way.

### C. Custody-bound signatures and challenges

The validator signs a new version of the sign bytes that also covers a salted Merkle root over its assigned rows:

```text
SignBytes_v1 = RawBytesMessageSignBytes(chain_id, "fibre/pp:v1", stripped_v0 || custody_root)

salt         = SHA256("fibre/custody:v1" || promise_hash || validator_address)
custody_root = RFC 6962 Merkle root over leaves (salt || u32be(j) || row[j])
               for the validator's assigned rows j, in Assign order
```

- **Why the salt is needed.** The row tree hashes each leaf as `SHA256(0x00 || row)`, and each row's proof contains its sibling's leaf hash. Rows are shuffled across validators (`Set.Assign`, `fibre/validator/set.go:65`), so one validator's leaf hashes sit inside other validators' proofs. A root over unsalted row hashes could be built without ever holding the rows. The salt makes every leaf unique to one promise and one validator, so the only way to compute it is to hash the row bytes.
- **Challenges.** Each block, the block hash selects `C` pairs of a paid signature and a row position. Within `N` blocks the validator submits `MsgCustodyResponse` with the row, its row proof and the custody path, about 33 KiB at the 128 MiB maximum blob size.
- **What it proves.** Whoever computed the root had the row bytes at signing time. That proves receipt strongly and retention only weakly, because a validator that deleted its rows can rebuild a challenged row from peers within `N` blocks. It does not prove that the validator serves reads.

### D. Penalties

These depend on extension C, which provides the evidence.

| Evidence                                                | Objective?                           | Consequence                |
|---------------------------------------------------------|--------------------------------------|----------------------------|
| A custody opening whose row is not in the commitment    | Yes, the consensus key signed it     | Slash                      |
| Two different custody roots for one promise             | Yes, the root is deterministic       | Slash                      |
| More than `M` of the last `W` challenges missed         | Attributable, but may be an outage   | Jail                       |
| Signature not on chain in time                          | No, the client and network affect it | Lose that blob's payment   |

Jailing for missed challenges should be as lenient as downtime jailing is today, which tolerates missing all but 10 of 10,000 blocks. A single miss never jails.

The rule in every phase is: **never slash for bandwidth.** A slow upload can always be blamed on the client, so bandwidth only decides who gets paid.

### E. Staking rewards tied to participation

A part of staking rewards is withheld from validators whose participation over an epoch is below a threshold. This is the only extension that works before Fibre fees are significant. It needs a participation metric that clients cannot distort, so it depends on extensions A and B.

## Block production cost

Block time does not change. The interval is set by timeouts (`DelayedPrecommitTimeout` 2.1 s + `TimeoutCommit` 0.5 s, `pkg/appconsts/app_consts.go`), not by processing time, so extra work matters only if ProcessProposal and FinalizeBlock run past that window. At target with 128 MiB blobs there are about 45 PFFs per block.

| Work                              | Where it runs                                                                                                                                   | On the critical path?                                                                                                 |
|-----------------------------------|-------------------------------------------------------------------------------------------------------------------------------------------------|-----------------------------------------------------------------------------------------------------------------------|
| Full signature verification       | CheckTx, in 128-signature ed25519 batches across all CPUs, cached (`x/fibre/ante/ante.go:104`, `x/fibre/keeper/preverify.go`). About 4,500 checks per block. | No. ProcessProposal hits the cache, and verifies a PFF it has not seen in parallel with its walk (`app/process_proposal.go:142`). |
| Payouts                           | End of block, at most one `AllocateTokensToValidator` per validator.                                                                            | Yes, but bounded at roughly double today's distribution work.                                                        |
| Block size                        | Up to 72 more 64-byte signatures per PFF, about +200 KB per block.                                                                              | Affects propagation only.                                                                                             |
| Phase 2: `MsgEndorse`             | Same CheckTx preverification and cache as `MsgPayForFibre`.                                                                                     | Only if that requirement is broken.                                                                                   |
| Phase 2: custody challenges       | One ~33 KiB response and one Merkle check per block at `C` = 1.                                                                                 | Negligible.                                                                                                           |

With 32 MiB blobs the target needs about 180 PFFs per block, close to the limit of 200 (`MaxPayForFibreMessages`, `pkg/appconsts/fibre_consts.go:7`), and about 18,000 signature checks per block.

These are estimates. Before mainnet, ProcessProposal, FinalizeBlock and CheckTx throughput must be benchmarked at target load (2.2 GiB/s of uploads, 128 MiB and 32 MiB blobs, about 100 validators), with warm and cold signature caches, using the Fibre benchmark configuration (`pkg/appconsts/fibre_consts_bench.go`). The acceptance bar is no measurable change in block interval and no more than a few percent added to ProcessProposal and FinalizeBlock time.

## Alternatives considered

- **Redistribute non-signer shares to signers.** This pays signers more when others fail, so a validator that also runs a client profits from skipping its competitors. Sending those shares to the community pool removes that profit.
- **Pay signers by stake instead of by rows.** It keeps today's split among signers, but small validators would still carry up to 12 times their stake share for no extra pay. See open question 1.
- **Authenticated sampling endpoint.** Each validator hardcodes a team public key and accepts probe traffic only from it. It is simple, but probes are identifiable by design: a validator can serve probes first or scale up only when probed. It measures an endpoint rather than real service, and the only enforcement is social.
- **Peer probing with on-chain reports.** Validators probe each other and post scores. Prober identities are public, a prober's uplink caps the test, failures cannot be attributed, and cartels can trade good reports.
- **Client receipts.** Clients sign per-validator latency receipts. Clients can lie and validators can self-deal.
- **Unsalted shard hash in the signature.** Fails for the reason given under extension C.

## Consequences

### Positive

- A validator's Fibre income depends on its own work. At the upload target that is about 144,000 TIA a day, paid only to validators that signed.
- Small validators are paid for the rows they carry.
- Per-validator participation and the network's reserve are public, computed from chain data alone.
- Phase 1 needs no new messages, no persistent state and no change to the signature format.

### Negative

- Phase 1 changes fee distribution and PFF validation, so it breaks consensus, and clients must be updated to include all signatures.
- Signature verification work grows 3 to 4 times, and PFFs grow by up to about 4.6 KB each.
- The grace period delays PFF submission.
- Paying by rows moves Fibre fees from large validators towards small ones.
- Validators far from the main clients sign later and lose more blobs until extension A is adopted.

### Residual risks

- **Lying signers are paid in phase 1.** A server that signs without storing collects fees until an off-chain sampler exposes it. Extension C closes this.
- **Stake splitting.** Paying by rows rewards splitting one validator into several small ones, because each gets the floor. The size of the bonded set limits this, and each split validator must still sign for its rows.
- **Clients decide who is paid in phase 1.** Extensions A and B close this.
- **Serving reads is never proven**, in either phase. A refusal to serve cannot be attributed on chain, so it stays with off-chain sampling.

## Open questions

1. **Payout weight: rows or stake.** Rows match pay to load and pay small validators for the floor, but reward stake splitting. Stake keeps today's split and has no splitting incentive.
2. **Non-signer shares: community pool or burn.**
3. **Grace period.** Start around 1 s and tune it from the measured distribution of signing delays.
4. **Staking rewards.** Whether any part of them should depend on Fibre participation (extension E).
5. **Load tests.** Who runs them, how often, and who pays.
6. **Row floor.** On mainnet the floor assigns 19,046 rows where 16,384 exist, so 2,662 rows are assigned twice. The duplicates sit with 18 validators holding 0.03% of stake, so recovery is unaffected. Is that acceptable as stake concentrates further?
7. **Rollout.** Start in shadow mode: verify all signatures and publish the record, but keep paying `fee_collector`, so operators see their participation before any money moves. Then turn on signer payments by parameter, after a run on Mocha at target load.

## References

- `fibre/validator/set.go:65,108`: `Assign` and `AssignedRows`.
- `fibre/protocol_params.go:58,136`: default protocol parameters and `MinRowsPerValidator`.
- `fibre/validator/signature_set.go:50`: `QuorumPrefix`.
- `fibre/client_upload.go:191`, `fibre/client_config.go:131`: signature collection at quorum and background uploads.
- `fibre/server_upload.go:112`: where the server signs.
- `x/fibre/keeper/msg_server.go:348,385`: the transfer to `fee_collector` and signature validation.
- `x/fibre/keeper/preverify.go`, `x/fibre/ante/ante.go:104`, `app/process_proposal.go:142`: signature preverification and cache.
- `pkg/appconsts/app_consts.go`, `pkg/appconsts/fibre_consts.go:7`: block timing and the PFF limit per block.
- `pkg/rsema1d/SPEC.md`: commitment and leaf hashing.
- cosmos-sdk `x/distribution/keeper/allocation.go`: `AllocateTokens`, `AllocateTokensToValidator`.
- `specs/src/fibre.md`, `specs/src/fibre_client.md`, `specs/src/fibre_module.md`, `specs/src/fibre_encoding.md`.
