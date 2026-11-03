# ADR 032: Fibre Delegated Server

## Changelog

- 2026-11-03: Initial draft

## Status

Proposed

## Summary

A validator can authorize a third party to run its Fibre server without handing
over its consensus key. The validator signs a **lease** with its consensus key
that names a separate ed25519 **delegate key** and a block-height validity
window. The lease is stored in the `x/valaddr` registry with a height-indexed
history. Everywhere Fibre verification accepts the consensus key today — on-chain
PayForFibre (PFF) settlement and download TLS — it also accepts the delegate key
that was active at the relevant height. The validator can issue, rotate, and
revoke the lease. The consensus key never leaves the validator.

## Context

Everything a validator does in Fibre is today rooted in its **consensus key**:

- the Fibre server signs each `PaymentPromise` endorsement with the consensus key
  (`fibre/server_upload.go` → `SignPaymentPromiseValidator`,
  `fibre/payment_promise.go`);
- the server's TLS identity is an ephemeral TLS key bound to the consensus key by
  a signed certificate extension (`fibre/internal/tlsid/tlsid.go`);
- on-chain PFF settlement verifies those endorsements against the consensus
  pubkey taken from staking historical info at the promise height
  (`x/fibre/keeper/validator_set_cache.go` `certificateValidatorSet`,
  `x/fibre/keeper/msg_server.go` `validateValidatorSignatures`).

The only way to let a third party run your Fibre server today is to give them the
consensus key, directly or through the remote signer the server already supports
(`fibre/internal/sign/grpc.go`, `fibre/server_config.go`). That is unacceptable:
the consensus key can double-sign blocks and get the validator slashed, it cannot
be rotated (`tlsid.go`: "a Celestia validator's consensus key cannot rotate"), and
the grant cannot be scoped or revoked. PROTOCO-3341 (KR1 milestone 2) asks for a
**separate, scoped, revocable** key the operator holds, so the operator can serve
uploads and downloads without ever touching the consensus key.

**The core constraint.** The signing key for a given promise is resolved
**historically, at the promise height**, from staking historical info (fix #8330
verifies against the historical set at the promise height; #8342 records historical
info before slashing so the recorded set matches CometBFT's signing set). But the
registry today is **current-state only**: one record per validator keyed by
consensus address (`x/valaddr/types/keys.go`, prefix `0x01`), overwritten in place
and garbage-collected in EndBlock (`RemoveStaleFibreProviders`). It cannot answer
"what was registered at height H." Any delegate design must make "validator →
active delegate key at height H" answerable historically for every settleable
height.

**What bounds the damage.** A promise only settles against the **user's** escrow
and only if the **user's own secp256k1 signature** is valid
(`x/fibre/keeper/msg_server.go` `PayForFibre`; `fibre/client_upload.go`
`signedPromise`). A delegate cannot fabricate a charge, touch stake, or forge
blocks. There is also **no slashing** for Fibre endorsements today — they are
trusted like blob share commitments (`x/fibre/README.md`) — and this design adds
none. The realistic harm from a compromised delegate is endorsing promises for
data that was not actually stored, which is the same harm a malicious validator
could already do with its consensus key, now reachable with a scoped key instead.

**Interaction with milestone 4 (multiple Fibre servers per validator,
PROTOCO-2997).** M4 wants several servers — and so several active delegate keys —
per validator at once, each counting once toward that validator's single
voting-power slot. To avoid a second state break, the M2 schema models a
validator's delegation as a **list of bindings**, even though M2 activates only
one at a time. Verification that accepts "any active binding, count power once" is
M4 work and is deferred (see Alternatives).

## Decision

### The lease

A validator authorizes a delegate with a **lease**: a statement, signed by the
validator's **consensus key**, that a given ed25519 delegate pubkey may sign Fibre
endorsements and present the server's TLS identity for a bounded **block-height
window**, carrying a monotonic nonce.

```proto
message FibreDelegation {
  bytes  delegate_pubkey = 1;  // ed25519; key_type reserved, MUST be ED25519 in v1
  uint32 key_type        = 2;
  int64  valid_from      = 3;  // inclusive block height
  int64  valid_until     = 4;  // exclusive block height
  uint64 nonce           = 5;  // monotonic per validator
  bytes  consensus_sig   = 6;  // consensus-key sig over the canonical lease bytes
}

message FibreProviderInfo {
  string host = 1;                        // unchanged (routing hint)
  repeated FibreDelegation delegations = 2;
}
```

The lease bytes signed by `consensus_sig` are a domain-separated encoding of
`(chain_id, validator_cons_addr, delegate_pubkey, key_type, valid_from,
valid_until, nonce)` via `core.RawBytesMessageSignBytes` with a new unique-ID tag
(mirroring `tlsid.SignUniqueID`). Binding the chain ID and validator address stops
a lease being replayed onto another chain or validator; the nonce stops a
superseded lease being re-submitted as current.

**ed25519 only in v1 (constraint K).** The delegate key must be ed25519 because
the server-side signature length is checked against `ed25519.SignatureSize`
(`fibre/payment_promise.go`), on-chain verification uses the ed25519 batch verifier
(`x/fibre/keeper/preverify.go`), and the converted set is built as `ed25519.PubKey`
(`x/fibre/keeper/validator_set_cache.go`). `key_type` is reserved for later but
validation rejects anything other than ED25519 now.

### State and history

The lease lives in `x/valaddr` next to the host. Because the current record is
overwritten in place, a per-validator, height-indexed **history** of superseded
bindings is kept under a new prefix so a historical promise height can be resolved:

```
0x02 | ConsAddress | BigEndian(valid_from) | BigEndian(nonce) -> FibreDelegation
```

The key includes the nonce, not just the activation height, so two bindings that
activate at the same height stay distinct.

**Identity binding.** A validator that has set no delegation is treated as having
an implicit identity binding (`delegate_pubkey = consensus pubkey`, nonce 0).
Migration and un-upgraded clients rely on this. To keep "never delegated" distinct
from "history pruned," an explicit identity binding is written at the delegation
epoch boundary, so a present entry means "consensus key" and an absent entry means
"pruned/unknown."

**Pruning.** A binding is retained until **no settleable promise can still
reference it** — that is `valid_until` plus the full acceptance window, where the
acceptance window is the larger of the normal `PaymentPromiseHeightWindow` path and
the timeout-settlement path (`MsgPaymentPromiseTimeout`, which skips the
height-window check). Pruning only at `valid_until + small margin` would delete a
key while a promise is still settleable. Separately, a minimum height gap between
`MsgSetFibreDelegation`s bounds how many records a validator can accumulate, since a
bounded height window does not by itself bound the record count.

### Messages: who submits, who authorizes

Two distinct concerns:

- **Submit** the transaction: the **operator account key** (`celestiavaloper…`),
  same as `MsgSetFibreProviderInfo` today, so the consensus key never builds or
  broadcasts transactions.
- **Authorize** the delegation: the **consensus key**, via `consensus_sig` inside
  the message. The handler verifies `consensus_sig` against the validator's current
  consensus pubkey before storing.

```proto
message MsgSetFibreDelegation {
  option (cosmos.msg.v1.signer) = "signer";
  string          signer     = 1;  // operator, pays/sequences the tx
  FibreDelegation delegation = 2;  // consensus-key-signed lease
}
message MsgRevokeFibreDelegation {
  option (cosmos.msg.v1.signer) = "signer";
  string signer = 1;               // operator account key alone may revoke
  uint64 nonce  = 2;               // the lease being revoked
}
```

Requiring the consensus-key signature on the lease protects specifically against
**operator-account-key compromise** redirecting Fibre signing authority. The
operator account key is hot and tx-signing (a lower bar than the consensus key),
and it can already move the `host`, so without this a compromised operator key
could also redirect signing. The consensus key signs a lease only occasionally —
not every promise — via the same `PrivValidatorAPI.SignRawBytes` path the server
already uses, or an offline signer. **Revocation is decoupled:** the operator
account key **alone** may revoke (never add or rotate), so a validator can kill a
delegation fast without reaching for the consensus key.

### Prospective activation (required for cache soundness)

A `MsgSetFibreDelegation` or `MsgRevokeFibreDelegation` committed in block `B` may
only take effect at `B+1` or later: a `valid_from ≤ B` is rejected (no backdating),
and a revocation records an independent `revoke_at = B+1` at the earliest. This is
not cosmetic. Backdating or same-block revocation would let the authorized delegate
for an already-committed height change *after* a certificate for that height was
built and possibly cached, so a cached certificate could pass while fresh
verification rejects it (or vice versa). With prospective activation, the
authorized delegate for any height `≤ current` is immutable, exactly like staking
historical info, and the existing per-height cache stays correct.

Precisely: the delegate that resolves a promise at height `H` is the binding whose
`valid_from ≤ H < min(valid_until, revoke_at)` in the state committed **at or
before** `H`. Revocation records `revoke_at` as a separate field and never mutates
the signed `valid_until`, so the signed lease bytes stay verifiable; the effective
end of a binding is `min(valid_until, revoke_at)`.

### On-chain verification

Fold the delegate substitution into the **existing per-height converted set**. In
`validatorSetAtHeight` / `certificateValidatorSet`
(`x/fibre/keeper/validator_set_cache.go`), after loading staking historical info at
the certificate height, for each validator look up its active delegation at the
promise height from the valaddr history. If one exists, build that validator's
`core.Validator` with the **delegate** ed25519 pubkey; otherwise use the consensus
pubkey as today. The result stays a `convertedValidatorSet`, so the per-height LRU
and the `preverify.go` signature cache continue to apply unchanged, and #8330/#8342
stay intact — the consensus set is still resolved at `H`/`H-1`, and the delegate is
layered on top of that exact set.

Two correctness requirements:

- **Pruned history must not silently fall back to the consensus key.** That would
  accept a consensus-key signature for a promise whose real authorized signer was a
  delegate. A delegation-history existence check (analogous to the `HasHistoricalInfo`
  fast path, #8292, `app/fibre_staking.go`) surfaces the same "set no longer stored"
  error as a pruned validator set.
- **Cache correctness is a tested invariant, not an assumption.** A delegation
  change committed at `B` must never alter the resolved set for any height `≤ B`.

### Client discovery and verification

- **Upload:** the client reads the validator set at the promise height and
  verifies returned signatures positionally
  (`fibre/validator/signature_set.go`), unchanged. It additionally reads the
  delegate pubkey active **at the promise height** and uses it as the expected
  `PubKey` for that validator's slot. Quorum (2/3), ordering, and the certificate
  wire format are unchanged.
- **Download:** downloads happen at **current time**, so the client reads the
  **currently active** delegate binding and sets it as `expected` for
  `tlsid.VerifyConnection` (`fibre/internal/grpc/fibre_client.go`). The server's
  TLS identity extension is signed by the delegate key instead of the consensus
  key.
- **A wrong delegate pubkey defeats TLS auth** — unlike a wrong `host`, which only
  redirects traffic that TLS still authenticates against an independently known
  key. So the client must either read the binding from **authenticated chain
  state**, or obtain the `consensus_sig` lease and **verify it itself** against the
  validator's consensus pubkey (which it already trusts from the validator set)
  before accepting the delegate key. Checking current revocation additionally needs
  fresh authenticated state or an explicitly trusted RPC.
- **Revocation reaches downloads within a bounded number of blocks, not
  instantly.** A registry change does not tear down existing or pooled TLS
  connections. The `HostRegistry` refresh (`fibre/internal/grpc/host_registry.go`)
  re-resolves and drops connections to a validator whose active delegate changed,
  giving a defined maximum authorization staleness.

### Upgrade path

- **State-breaking**, gated behind the next hard fork (**app version 12**; current
  is 11, `pkg/appconsts/app_consts.go`). New messages are rejected below v12.
- **Migration** (pattern: `x/fibre/keeper/migrations.go`) leaves every existing
  `FibreProviderInfo.host` as-is and adds no explicit binding, so existing
  validators keep the implicit identity binding and behave byte-for-byte the same
  until they opt in. No history entries are created at migration.
- **Opt-in per validator.** While a validator has only the identity binding, an
  un-upgraded client that ignores `delegations` still verifies against the
  consensus key. The moment a validator sets a non-identity delegate, only clients
  that read the delegate pubkey can verify its endorsements and downloads, so a
  validator should delegate only once enough clients understand delegation.

## Consequences

- A third party can run a validator's Fibre server with a scoped, revocable
  ed25519 key. The consensus key never leaves the validator, so there is no
  double-sign or slashing exposure from delegation — the central improvement over
  the status quo.
- **Residual exposure after revocation.** A promise's height does not prove when
  the endorsement was produced. A compromised delegate can keep signing after
  revocation for any promise whose height still falls inside its former authorized
  window and is still settleable. Short lease windows reduce but do not eliminate
  this. This must be stated plainly to operators.
- Promises authorized while the delegate was active still settle after revocation.
  This is intended: the user already signed them and a 2/3 quorum already endorsed
  them; there is no retroactive invalidation, by design.
- **Operator concentration is a storage-guarantee risk the chain cannot measure.**
  One operator holding delegate keys for ≥ 1/3 of voting power can withhold data;
  ≥ 2/3 can supply a full quorum of endorsements without storing the data. This is
  not a block-consensus-safety risk (stake and block production are untouched), but
  distinct delegate keys hide common ownership, so the registry cannot measure real
  operator independence. Treat operator independence as a social/governance
  assumption; do not claim the registry makes concentration observable.
- Extra state: a history store prefix per validator and bounded per-validator
  growth (rate-limited by the minimum change gap).
- New verification-critical surface — the `consensus_sig` path and the canonical
  lease encoding — deserves the same golden-vector treatment as `tlsid`
  (`specs/src/fibre_tls_identity.md`, `testdata/identity_vectors.json`).
- No change for validators that do not delegate.

## Alternatives

- **A. Off-chain certificate chain in the TLS/gRPC handshake (no registry
  change).** The operator presents a validator-signed cert in the handshake. *Does
  not solve the on-chain PFF path*: the state machine must resolve the delegate at
  the promise height, and an off-chain-only cert is invisible to it.
- **A′. Carry the lease inside the PFF (self-contained certificate).** Embed the
  consensus-key-signed lease in `MsgPayForFibre`; verification resolves only the
  consensus key at the promise height (existing #8330/#8342 path), verifies the
  lease under it, checks the window, and verifies the delegate signatures — **no
  history store, no pruning, no historical-registry lookup**. Cost: lease bytes on
  every PFF, and immediate revocation still needs on-chain state. This is a genuine
  toss-up with the recommendation and is an open question. The recommendation leads
  because the binding must live in the registry anyway for discovery and download
  auth, so putting the authoritative lease there avoids duplicating it into every
  settlement; a reasonable hybrid uses the registry for discovery/downloads and
  either resolution method for on-chain upload verification.
- **B. Reuse the consensus key on the operator host (status quo).** Point the
  server's remote signer at the operator. Zero protocol change, but the operator
  gains consensus-key signing: a compromise double-signs blocks → slashing, and the
  key cannot be rotated. Acceptable only as a stopgap, not as delegation.
- **C. Registry with a delegate pubkey but no window/nonce/history.** Simplest
  change, but no bounded revocation, no rotation, and — fatally — no historical
  resolution, so changing the key breaks verification of promises signed before the
  change. Fails the core constraint.
- **Concurrent two-key overlap for rotation.** Making both the old and new key
  valid for a validator's single slot over an overlap band is "accept any of N
  active keys, count power once" — it cannot be done by substituting one pubkey and
  needs multi-candidate batch verification. That is the M4 shape. M2 ships a
  **scheduled single-key cutover** instead: the new lease has a later window and
  higher nonce, old `valid_until = C`, new `valid_from = C`, exactly one key active
  at every height, with a one-block seam at `C` rather than a true overlap.

## Open questions

1. **Resolution method:** registry-with-history (recommended) vs lease-in-PFF (A′),
   which drops the history store at the cost of lease bytes per settlement. Decide
   before building the verification PRs.
2. **Lease authority:** confirm the consensus-key-signed lease (recommended) over
   operator-account-key authorization, and confirm the operator account key alone
   may revoke.
3. **Height vs time windows:** the lease window is in block heights; promise
   *freshness* stays time-based. Is the mismatch acceptable, or should the lease
   also be expressed in time for operator ergonomics?
4. **Max lease length and history retention:** tie retention to
   `PaymentPromiseHeightWindow` (1000) vs `HistoricalEntries` (10000); confirm the
   timeout-settlement path needs no longer window. What is the maximum lease length?
5. **M4 now or later:** build the repeated-binding schema and any-active-key
   verification now to avoid a second state break, or ship a single binding and
   break again at M4?
6. **Accountability:** keep the current "no slashing, trusted like share
   commitments" posture, or add reporting/slashing for delegate misbehavior?

## References

- PROTOCO-3341: Design — validators delegate running the Fibre server (KR1
  milestone 2).
- PROTOCO-2997: Multiple Fibre servers per validator (milestone 4).
- Design doc: `docs/plans/q4-okr/delegated-fibre-server.md`.
- `x/valaddr/types/keys.go`, `x/valaddr/keeper/keeper.go` — registry state.
- `x/fibre/keeper/validator_set_cache.go`, `x/fibre/keeper/preverify.go`,
  `x/fibre/keeper/msg_server.go` — certificate verification and caching.
- `fibre/server_upload.go`, `fibre/payment_promise.go` — endorsement signing.
- `fibre/internal/tlsid/tlsid.go`, `fibre/internal/grpc/fibre_client.go`,
  `fibre/internal/grpc/host_registry.go` — TLS identity and download auth.
- `fibre/validator/signature_set.go` — positional certificate and 2/3 quorum.
- `app/fibre_staking.go` (`HasHistoricalInfo`, #8292); fixes #8330, #8342.
- ADR-025 (Fibre local promise cache), ADR-030 (elastic shard storage).
