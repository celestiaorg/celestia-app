# ADR 032: Fibre Delegated Server

## Changelog

- 2026-11-03: Initial draft
- 2026-11-03: Decouple the delegate key from validator identity; separate delegation
  state from the host record; tighten rotation, retention, and download freshness
  (review round 1)
- 2026-11-03: Bind delegate endorsements to the validator's consensus address; thread
  the verify key and sign-bytes through the pre-verification batch path; require fresh
  authenticated state for download revocation; bounded history-floor rule (review round 2)

## Status

Proposed

## Summary

A validator can authorize a third party to run its Fibre server without handing
over its consensus key. The validator signs a **lease** with its consensus key
that names a separate ed25519 **delegate key** and a block-height validity
window. The lease is stored in the `x/valaddr` registry with a height-indexed
history. Everywhere Fibre verification accepts the consensus key today — on-chain
PayForFibre (PFF) settlement and download TLS — it also accepts the delegate key
that was active at the relevant height. Crucially, the delegate key is used only
to **verify signatures**: the validator's on-chain identity, voting power, and
quorum accounting stay rooted in the consensus key. The validator can issue,
rotate, and revoke the lease. The consensus key never leaves the validator.

## Context

Everything a validator does in Fibre today is rooted in its **consensus key**:

- the Fibre server signs each `PaymentPromise` endorsement — a validator's attestation
  that it stored a user's data — with the consensus key
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

**Identity is derived from the signing key — the central design hazard.** In
CometBFT a validator's `Address` is derived from its pubkey. Three places depend on
this: `core.NewValidator` sets `Address = pubKey.Address()`; `core.NewValidatorSet`
**panics on a duplicate Address**; and `SignatureSet.Add` both verifies the signature
against `val.PubKey` *and* dedups and accounts quorum by `val.Address`
(`x/fibre/keeper/validator_set_cache.go`, `fibre/validator/signature_set.go`). So a
delegate key cannot simply be swapped in for the consensus key: that would change
the validator's identity, not just the key its signature verifies under. The design
below keeps identity and the verification key separate for exactly this reason.

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
```

The lease bytes signed by `consensus_sig` are a domain-separated encoding of
`(chain_id, validator_cons_addr, delegate_pubkey, key_type, valid_from,
valid_until, nonce)` via `core.RawBytesMessageSignBytes` with a new unique-ID tag
(mirroring `tlsid.SignUniqueID`). Binding the chain ID and validator address stops
a lease being replayed onto another chain or validator; the nonce stops a
superseded lease being re-submitted as current.

**Delegate endorsements are bound to the validator's consensus address.** Today a
Fibre endorsement's sign-bytes are the canonical promise bytes alone, carrying no
endorsing-validator identity (`fibre/payment_promise.go`
`RawBytesMessageSignBytes`); identity comes only from the fact that each validator
holds a unique consensus key. Under delegation that is no longer enough: if two
validators share a delegate key, one honest server's signature could be copied into
both of their positional slots (each validator's fixed position in the ordered
certificate), because the bytes verify under either slot's key and quorum counts
distinct consensus addresses (`fibre/validator/signature_set.go`). The result is
quorum without the data being stored. So a **delegate-signed** endorsement
is domain-separated by the signing validator's consensus address: its sign-bytes are
`(promise_sign_bytes, validator_cons_addr)` under a distinct tag. A given signature
is then valid for exactly one slot even when the key is shared. Consensus-key
endorsements are unchanged (the unique key already binds identity), so this is purely
additive at v12. The sign-bytes for a slot are therefore resolved **per slot** from
the active binding, alongside the verify key (see On-chain verification).

**ed25519 only in v1 (constraint K).** The delegate key must be ed25519 because
the server-side signature length is checked against `ed25519.SignatureSize`
(`fibre/payment_promise.go`), on-chain verification uses the ed25519 batch verifier
(`x/fibre/keeper/preverify.go`), and the converted set (the CometBFT validator set
rebuilt for signature verification) is built as `ed25519.PubKey`
(`x/fibre/keeper/validator_set_cache.go`). `key_type` is reserved for later but
validation rejects anything other than ED25519 now.

### State and history

Delegation state is kept **separate from the host record**, because the host record
(`FibreProviderInfo`, prefix `0x01`) is overwritten in place by
`MsgSetFibreProviderInfo` and deleted by the EndBlock stale-provider sweep; storing
delegations inside it would let a routine routing update, or a brief unbonding, wipe a
validator's bindings and nonce high-water (the highest nonce ever accepted).
`FibreProviderInfo.host` is unchanged.
Two new stores:

```
0x02 | ConsAddress | BigEndian(valid_from) | BigEndian(nonce) -> FibreDelegation   // history
0x03 | ConsAddress -> FibreDelegationState { repeated FibreDelegation active; uint64 nonce_high_water }
```

The `0x03` state holds the active binding(s) (one in M2, a list for M4) and a
monotonic `nonce_high_water` that routing updates and the stale-provider sweep must
never touch. The `0x02` history key includes the nonce, not just the activation
height, so two bindings that activate at the same height stay distinct. Delegation
state has its **own lifecycle**, keyed to the settleable window (below), independent
of whether the validator currently has a host record or is bonded.

**Never-delegated vs pruned (bounded history-floor rule).** A validator that has set
no delegation is treated as having an implicit identity binding (`delegate_pubkey =
consensus pubkey`). Migration and un-upgraded clients rely on this. The hazard is
telling "never delegated" (safe to fall back to the consensus key) apart from
"history was pruned" (must error, since the real signer may have been a delegate).
Rather than write an identity binding every epoch — unbounded churn across the whole
set — the `0x03` state records, per validator that has **ever** delegated, a single
`history_floor` height (the lowest height still retained). Resolution at promise
height `H` then: if the validator has no `0x03` state at all, it never delegated →
consensus-key fallback; if it has state and `H ≥ history_floor`, resolve from `0x02`
(a gap within the retained range is an explicit identity interval); if it has state
and `H < history_floor`, the history was pruned → **error**, never fall back. This is
one bounded field per delegating validator, not a recurring write.

**Pruning.** A binding is retained until **no settleable PFF promise can still
reference it** — that is its immutable `effective_end`
(`min(valid_until, revoke_at, rotation_close)`, defined under Messages)
plus `PaymentPromiseHeightWindow`. Only the `MsgPayForFibre` certificate path
resolves a delegate at the promise height; `MsgPaymentPromiseTimeout` performs no
validator-signature verification and never resolves a validator set
(`x/fibre/keeper/msg_server.go`), so it imposes no delegation-history requirement.
Retention is therefore bounded by the height window alone. One caveat: the window is
a governance parameter (`UpdateFibreParams`), and raising it could re-admit heights
whose history was already pruned. The history-floor rule fails safe (those promises
error rather than mis-verify), but to preserve the retention *guarantee* either pin
delegation retention to a fixed maximum window independent of the live parameter, or
make the settleable-height floor monotonic. Flagged in open question 4.

**Bounding stored state.** A bounded height window does not by itself bound the
record count, so: (a) a maximum lease length; (b) a bounded scheduling horizon —
`valid_from` may not be more than a fixed number of blocks ahead of the committed
height; (c) at most one pending (not-yet-active) binding per validator; (d) a
minimum height gap between `MsgSetFibreDelegation`s. Together these bound the
retained records per validator.

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

The handler reconstructs the canonical lease sign-bytes from the **tx-signer's own
resolved consensus address** and stores the binding under that `ConsAddress`, so a
lease naming another validator's `validator_cons_addr` cannot be stored under the
wrong key, and verifies `consensus_sig` against that validator's current consensus
pubkey. `nonce` must exceed the stored `nonce_high_water`. `MsgRevokeFibreDelegation`
likewise resolves its target validator from the tx-signer's own consensus address, so a
revoke can only touch that signer's own binding, never another validator's.

Requiring the consensus-key signature on the lease protects specifically against
**operator-account-key compromise** redirecting Fibre signing authority. The
operator account key is hot and tx-signing (a lower bar than the consensus key),
and it can already move the `host`, so without this a compromised operator key
could also redirect signing. The consensus key signs a lease only occasionally —
not every promise — via the same `PrivValidatorAPI.SignRawBytes` path the server
already uses, or an offline signer. **Revocation is decoupled:** the operator
account key **alone** may revoke (never add or rotate), so a validator can kill a
delegation fast without reaching for the consensus key.

**Rotation is one atomic transition.** A `MsgSetFibreDelegation` that installs a
replacement binding also prospectively closes the prior active binding by recording a
`rotation_close` equal to the new binding's `valid_from` (the signed `valid_until` is
never mutated). The handler rejects a new binding whose `[valid_from, valid_until)`
overlaps an existing pending binding, so there is never more than one authoritative
binding per height to resolve. This gives a clean scheduled cutover with exactly one
active key at every height.

**Single effective-end field.** Each history entry stores one
`effective_end = min(valid_until, revoke_at, rotation_close)`, written when any of the
three is set and never un-set. All resolution and pruning use this single field, so
rotation, revocation, and natural expiry cannot disagree.

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
`valid_from ≤ H < effective_end` in the state committed **at or
before** `H`. Revocation records `revoke_at` as a separate field and never mutates
the signed `valid_until`, so the signed lease bytes stay verifiable; the effective
end of a binding is its single `effective_end` field (above).

### On-chain verification: identity stays with the consensus key

Validator Address, ordering, voting power, and quorum dedup are all derived from the
consensus key, so rebuilding the converted set with delegate pubkeys would be unsafe.
Two validators can share a delegate key: a Byzantine validator can force this by naming
another validator's pubkey as its delegate, and the one-operator-one-key-many-validators
case produces it naturally. Shared keys would then collide on `Address`, causing either a
**panic in CheckTx / ProcessProposal (chain halt)** via `core.NewValidatorSet` or a
**silent quorum under-count** via `SignatureSet.Add`'s Address-keyed dedup.

Instead, keep the converted set exactly as today (identity and power from the
consensus key) and resolve, **per slot**, from the active binding at the promise
height, both:

- `verifyKey[i]` — the ed25519 pubkey slot *i*'s signature must verify under: the
  delegate active at the promise height if one exists, else the consensus pubkey; and
- `signMode[i]` — the sign-bytes format for that slot: **address-bound**
  `(promise_sign_bytes, validator_cons_addr)` for a non-identity delegate slot, or
  **legacy** promise bytes for a consensus slot (see The lease).

The resolved `(verifyKey, signMode)` arrays are a pure function of the **promise
height** and the state committed at or before it, so they can share the per-height LRU
machinery (`validator_set_cache.go`) — but keyed by the promise height, not by the
`certificateHistoricalHeight` (`promiseHeight-1`) the existing converted-set cache uses,
which folds `G` and `G+1` onto the same entry at genesis. (Harmless today, since v12
activates far above genesis; a genesis-boundary test records that.) The cache holds
**only keys and modes, never the actual sign-bytes**: those depend on the specific promise (commitment, signer, timestamp) and
are constructed per promise, in both verification paths, from the promise bytes plus —
for an address-bound slot — the consensus address. Address, ordering, and power are
untouched. Delegate-key reuse across validators is then harmless: distinct consensus
identities, and the address binding makes each signature valid for exactly one slot.

**The same per-slot mode is used by every producer and consumer of an endorsement —
five touchpoints — or signatures made under one format are rejected under another:**
the server signer (`SignPaymentPromiseValidator` takes the active binding to pick both
the key and the mode), the client's `SignatureSet` (reworked to carry a per-slot
message, not one shared `requiredBytesSigned`), the on-chain sequential verifier, the
on-chain batch path, and client discovery. A delegated server signs address-bound bytes
with its delegate key; a validator signing with its own consensus key signs legacy
bytes.

**Both verification paths must consume the same per-slot `(verifyKey, signBytes)`.**
There are two consensus-critical verifiers and they must implement an identical
acceptance predicate, or settlement becomes non-deterministic:

- the sequential verifier `validateValidatorSignatures` / `SignatureSet.Add`
  (`fibre/validator/signature_set.go`) — checks each signature against
  `verifyKey[i]` over the per-slot sign-bytes built from `signMode[i]`, while keying
  dedup and quorum by the consensus `Address`; and
- the **pre-verification batch path** `appendCertificateItems`
  (`x/fibre/keeper/preverify.go`), which today batch-verifies against
  `converted.validators[i].PubKey` (the consensus key) and a single message, warming
  the shared `pffSigCache`. The ante cache-hit branch (`x/fibre/ante/ante.go`) then
  skips full re-verification. If only the sequential path were delegate-aware, a
  warm cache and a cold cache would accept different certificates for the same input,
  splitting ProcessProposal votes. So the batch path is a **mandatory** consumer of
  the per-slot `verifyKey[i]` and `signMode[i]` (per-slot keys and per-slot messages,
  where today it uses one key column and one shared message).

This is a real, contained signature-path change across both paths — **not** a drop-in
substitution, and the implementation plan budgets for it. #8330/#8342 stay intact: the
consensus set is resolved at `H`/`H-1`, and the per-slot arrays are layered on top.

Three correctness requirements:

- **Pruned history must not silently fall back to the consensus key** (history-floor
  rule above). The check applies on both the derive/verify path and the **cache-hit
  fast path**: `ValidateValidatorSetAvailable` (`x/fibre/keeper/msg_server.go`
  `checkHistoricalInfo`) today asserts only staking historical info; it must also
  assert delegation-history availability for a non-identity binding, since delegation
  retention and staking `HistoricalEntries` have different windows. Otherwise a cached
  certificate at a height with pruned delegation history but surviving staking info
  could settle without re-asserting the binding. Because retention differs per binding,
  this must re-resolve **each signing slot's** binding at `H` (state reads, no crypto)
  and error if any is below `history_floor`; the fast path saves the signature crypto,
  not the state reads. `ValidateValidatorSetAvailable` therefore gains the signed-slot
  set as an argument (the ante already has `msg` in scope at the call site).
- **Warm-cache predicate == cold predicate under delegation** is the tested invariant.
  Extend the existing preverify fuzz cross-check (preverify vs
  `ValidatePayForFibreSignatures`) to cover active delegations, rotation, and
  revocation.
- **Cache correctness.** A delegation change committed at `B` must never alter the
  resolved arrays for any height `≤ B` (guaranteed by prospective activation).

### Server and operator identity

The delegated server signs with the delegate key, but its **identity on the network
stays the validator's consensus address.** Today the server derives its identity from
the signing key in several places: shard assignment (`fibre/server_upload.go`
`verifyAssignment` looks the validator up by `s.signer.GetPubKey().Address()`), the
object-storage namespace (`fibre/server.go`
`ObjectStorage.ValidatorAddress = ConsAddress(pubKey.Address())`), the budget
recompute (`recomputeBudget` → `valSet.GetByAddress(key.Address())`), and the health
dependencies. If the signer were the delegate key, assignment would fail ("validator
not in set"), the storage namespace would change on every rotation, and the budget
would stay zero. So the server must be configured with the validator's **consensus
address** as a stable identity, verify the delegate↔validator relationship on-chain
at startup, and use the consensus address **everywhere identity is derived** —
assignment, storage, budget, health; the delegate key is used only to sign.

**Signing key is selected by promise height.** The server signs each endorsement with
the delegate key that was active at the **promise's height**, not merely its current
key. This matters at a rotation cutover `C`: an in-flight promise with height `< C`
reaching the new operator after `C` must be signed with the old key (or rejected if
the new operator does not hold it). A cutover therefore drains or rejects
pre-cutover promises rather than signing them with a key that on-chain verification
will reject for that height. The server also presents one static TLS certificate built
at startup (`fibre/server.go`), so a cutover must replace that certificate (the TLS
identity is signed by the new delegate key) at activation, or hand over through a
coordinated restart and endpoint switch with a readiness check before cutover.

### Client discovery and verification

- **Upload:** the client reads the validator set at the promise height and
  verifies returned signatures positionally
  (`fibre/validator/signature_set.go`). It additionally reads the delegate pubkey
  active **at the promise height** and uses it as the per-slot verify key **and the
  matching per-slot sign-mode** (address-bound for a delegate slot), while keeping
  identity and quorum keyed by the consensus address. Quorum (2/3), ordering, and the
  certificate wire format are unchanged.
- **Download:** downloads happen at **current time**, so the client reads the
  **currently active** delegate binding and uses it as the `expected` key for
  `tlsid.VerifyConnection` (`fibre/internal/grpc/fibre_client.go`). Host resolution
  stays keyed by the **consensus address** (`host_registry` queries by
  `ConsAddress(val.Address.Bytes())`); the delegate key is passed separately from
  identity, exactly as on-chain, so it does not break host lookup. The server's TLS
  identity extension is signed by the delegate key instead of the consensus key.
- **A wrong delegate pubkey defeats TLS auth** — unlike a wrong `host`, which only
  redirects traffic that TLS still authenticates against an independently known key.
  Verifying the `consensus_sig` lease against the validator's consensus pubkey (which
  the client already trusts from the validator set) proves the key *was authorized for
  its window* — but **not** that it is still current. Rotation and revocation shorten
  authorization through a superseding binding and `revoke_at`, which live outside the
  signed lease, so a malicious registry plus a compromised former delegate could
  replay the authentic old lease until its original `valid_until`. Current-time
  download auth therefore requires **fresh authenticated chain state** for the active
  binding and its effective end; a self-verified lease alone is not sufficient.
- **Revocation must fail closed.** A registry change does not tear down existing or
  pooled TLS connections, and both `HostRegistry` and the client connection cache
  keep stale data when a refresh or replacement fails. Authorization must not
  inherit that fail-open behavior: define a maximum authorization staleness, refuse
  to use a delegate binding older than that deadline, and invalidate affected
  connections independently of replacement-server readiness. This gives revocation a
  bounded reach to live downloads.

### Upgrade path

- **State-breaking**, gated behind the next hard fork (**app version 12**; current
  is 11, `pkg/appconsts/app_consts.go`). New messages are rejected below v12.
- **Migration** (pattern: `x/fibre/keeper/migrations.go`) leaves every existing
  `FibreProviderInfo.host` as-is and creates no delegation state, so existing
  validators keep the implicit identity binding and behave byte-for-byte the same
  until they opt in.
- **Opt-in per validator.** While a validator has only the identity binding, an
  un-upgraded client that ignores delegations still verifies against the consensus
  key. The moment a validator sets a non-identity delegate, only clients that read
  the delegate pubkey can verify its endorsements and downloads, so a validator
  should delegate only once enough clients understand delegation.

## Consequences

- A third party can run a validator's Fibre server with a scoped, revocable
  ed25519 key. The consensus key never leaves the validator, so there is no
  double-sign or slashing exposure from delegation — the central improvement over
  the status quo.
- **This is not a drop-in change.** Keeping identity separate from the verification
  key means per-slot `verifyKey` / `signBytes` arrays through `certificateValidatorSet`
  consumed by **both** the sequential verifier (`SignatureSet.Add` /
  `validateValidatorSignatures`) **and** the pre-verification batch path
  (`appendCertificateItems`), a delegation-aware availability check on the ante
  cache-hit path, a stable-identity config for the server, and a client that splits
  host lookup (consensus address) from the verify key (delegate). The "fold into the
  existing converted set, nothing else changes" idea is rejected precisely because it
  corrupts identity and because the two verification paths must stay predicate-identical.
- **Residual exposure after revocation.** A promise's height does not prove when the
  endorsement was produced, so a compromised delegate can keep minting endorsements
  **after** revocation for any promise whose height still falls inside its former
  window and is still settleable (≈ `PaymentPromiseHeightWindow` blocks). Short lease
  windows do not help, because the attack targets *past* in-window heights. On-chain
  revocation therefore stops only **future-height** authority and the download-TLS
  flip; with no slashing it does not contain a leaked key within the settleable
  window. This must be stated plainly to operators.
- Promises authorized while the delegate was active still settle after revocation.
  This is intended: the user already signed them and a 2/3 quorum already endorsed
  them; there is no retroactive invalidation, by design.
- **Operator-key-alone revoke is a kill switch.** Letting the operator account key
  revoke without the consensus key means a compromised hot operator key can revoke the
  live delegation and re-point the `host`, disabling the validator's Fibre server
  (future-height authority plus the download-TLS flip). This is a griefing/DoS surface,
  not a signing-theft one — the attacker cannot forge endorsements or redirect signing
  authority — and is the accepted cost of fast revocation without the consensus key.
- **Operator concentration is a storage-guarantee risk the chain cannot measure.**
  One operator holding delegate keys for ≥ 1/3 of voting power can withhold data;
  ≥ 2/3 can supply a full quorum of endorsements without storing the data. This is
  not a block-consensus-safety risk (stake and block production are untouched), but
  distinct delegate keys hide common ownership, so the registry cannot measure real
  operator independence. Treat operator independence as a social/governance
  assumption; do not claim the registry makes concentration observable.
- **Operator switch needs a data handoff.** Because downloads authenticate against
  the current delegate, a cutover immediately excludes the former operator even if it
  holds the only copy of still-retained shards. A rotation procedure must transfer
  and verify retained shards before cutover, or both operators must share the same
  object-storage backend (ADR-030), so a switch does not orphan data.
- Extra state: two store prefixes (history and active/nonce) with bounded
  per-validator growth.
- New verification-critical surface — the `consensus_sig` lease encoding **and the
  address-bound delegate-endorsement sign-bytes** — deserves the same golden-vector
  treatment as `tlsid` (`specs/src/fibre_tls_identity.md`,
  `testdata/identity_vectors.json`), including a test that the three domain tags
  (`fibre/pp:v0`, the lease tag, the delegate-endorsement tag) are mutually distinct.
- **A gap or lapse reverts to the consensus key.** If a lease lapses or a rotation
  leaves a gap, resolution treats that interval as the identity (consensus) binding,
  which the delegate operator cannot serve — a validator not running its own server
  then produces no endorsements there. Operability footgun, not a safety issue;
  tooling should warn on gaps.
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
  height-indexed delegate-history store, no pruning, no pruned-vs-never ambiguity**.
  Cost: lease bytes on every PFF, the same per-slot verify-key/sign-mode threading as
  the recommendation, and — note — it is **not** zero on-chain state: to keep the same
  revocation semantics (a self-contained lease cannot tell a pre- from a post-
  revocation promise) it still needs a small retained per-lease effective-end /
  revocation record, or it accepts weaker, expiry-only revocation. It shares the same
  identity-decoupling requirement as the recommendation (the delegate is still only a
  verify key). This is a genuine toss-up and is open question 1. The recommendation
  leads because the binding must live in the registry anyway for discovery and
  download auth, so putting the authoritative lease there avoids duplicating it into
  every settlement; a reasonable hybrid uses the registry for discovery/downloads and
  lease-in-PFF for on-chain upload verification. **Two review rounds weighted this
  toward the hybrid:** the pre-verification threading, history pruning, pruned-vs-never
  rule, and cache-availability divergence — the most error-prone new consensus-critical
  surface — exist *only* in registry-with-history; A′ removes all of it while still
  needing the same identity decoupling and per-slot verify-key/sign-bytes threading.
  Registry-with-history is kept as the recommendation mainly because it generalizes
  cleanly to M4 (multiple active bindings); if M4 is deferred, the hybrid is likely the
  better M2 choice. This is the first open question.
- **B. Reuse the consensus key on the operator host (status quo).** Point the
  server's remote signer at the operator. Zero protocol change, but the operator
  gains consensus-key signing: a compromise double-signs blocks → slashing, and the
  key cannot be rotated. Acceptable only as a stopgap, not as delegation.
- **C. Registry with a delegate pubkey but no window/nonce/history.** Simplest
  change, but no bounded revocation, no rotation, and — fatally — no historical
  resolution, so changing the key breaks verification of promises signed before the
  change. Fails the core constraint.
- **Concurrent two-key overlap for rotation.** Making both the old and new key valid
  for a validator's single slot over an overlap band is "accept any of N active keys,
  count power once" — it cannot be done with a single per-slot verify key and needs
  multi-candidate batch verification. That is the M4 shape. M2 ships the scheduled
  single-key cutover above: old `valid_until = C`, new `valid_from = C`, and the
  half-open interval `valid_from ≤ H < …` resolves `H = C` cleanly to the new key, so
  exactly one key is active at every height with no gap.

## Open questions

1. **Resolution method:** registry-with-history (recommended) vs lease-in-PFF (A′),
   which drops the history store at the cost of lease bytes per settlement. Both need
   the same identity decoupling. Decide before building the verification PRs.
2. **Lease authority:** confirm the consensus-key-signed lease (recommended) over
   operator-account-key authorization, and confirm the operator account key alone may
   revoke. (The design doc's revoke message additionally carries a `consensus_sig`;
   this ADR drops it — confirm.)
3. **Is a separate revoke message worth it?** Given that revocation only stops
   future-height authority and flips download TLS, and cannot contain a leaked key
   within the settleable window, is `MsgRevokeFibreDelegation` + `revoke_at` worth
   the complexity over short windows plus non-renewal?
4. **Windows and retention:** the lease window is in block heights while promise
   *freshness* stays time-based — acceptable, or express the lease in time too for
   operator ergonomics? Confirm retention bounded by `PaymentPromiseHeightWindow`,
   and pick the maximum lease length and scheduling horizon.
5. **M4 now or later:** build the repeated-binding schema and any-active-key
   verification now to avoid a second state break, or ship a single binding and break
   again at M4?
6. **Operator switch:** require a verified shard handoff before cutover, or mandate a
   shared object-storage backend (ADR-030) for delegated servers?

## References

- PROTOCO-3341: Design — validators delegate running the Fibre server (KR1
  milestone 2).
- PROTOCO-2997: Multiple Fibre servers per validator (milestone 4).
- Design doc: `docs/plans/q4-okr/delegated-fibre-server.md`.
- `x/valaddr/types/keys.go`, `x/valaddr/keeper/keeper.go`,
  `x/valaddr/keeper/msg_server.go` — registry state and lifecycle.
- `x/fibre/keeper/validator_set_cache.go`, `x/fibre/keeper/preverify.go`,
  `x/fibre/keeper/msg_server.go` — certificate verification and caching.
- `fibre/server_upload.go`, `fibre/server.go`, `fibre/payment_promise.go` —
  endorsement signing and server identity.
- `fibre/internal/tlsid/tlsid.go`, `fibre/internal/grpc/fibre_client.go`,
  `fibre/internal/grpc/host_registry.go` — TLS identity and download auth.
- `fibre/validator/signature_set.go` — positional certificate and 2/3 quorum.
- `app/fibre_staking.go` (`HasHistoricalInfo`, #8292); fixes #8330, #8342.
- ADR-025 (Fibre local promise cache), ADR-030 (elastic shard storage).
