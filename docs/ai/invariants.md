# AI Code Generation Invariants

These invariants must hold for every code change in celestia-app. AI agents must read this file before generating code and must not produce code that violates any invariant. If a task cannot be completed without violating one, stop and ask the engineer.

Scope: celestia-app only. Networking and consensus internals (peer channels, reactors, gossip) are covered by the celestia-core equivalent of this document.

## INV-1: State machine determinism

**Statement:** All code reachable from ABCI (`PrepareProposal`, `ProcessProposal`, `FinalizeBlock`, `BeginBlocker`/`EndBlocker`, upgrades and migrations) must produce identical results on every node.

**Why:** Any divergence causes app hash mismatches, which halt the chain.

**Concrete rules:**

- No iteration over Go maps where order affects state, events, or gas consumption. Sort keys first.
- No `time.Now()` in state transitions. Use the block time from the context.
- No randomness, floats, or goroutine timing in state transitions.
- No node-local configuration or environment values influencing state transitions.

**Where it bites:** `x/*/keeper`, `app/ante/`, `app/prepare_proposal.go`, `app/process_proposal.go`, `app/upgrades.go`, state migrations.

**How to verify:** Scan the diff for map iteration, `time.Now`, `rand`, and float arithmetic in ABCI-reachable code. Trace each hit to confirm whether it can affect state, events, or gas.

## INV-2: All input is adversarial

**Statement:** Every transaction, message, and proposed block is attacker-controlled. Validate before use; bound before allocating.

**Why:** Unvalidated input in a message handler or proposal handler is a direct attack surface on every node.

**Concrete rules:**

- Validate every message field in message servers and ante decorators before acting on it.
- Check sizes and counts against protocol limits before allocating memory for them.
- Use overflow-safe arithmetic on user-controlled values.
- Transactions entering `PrepareProposal` come from the mempool and are untrusted. Blocks entering `ProcessProposal` come from the proposer and are untrusted.

**Where it bites:** `x/*/keeper` message servers, `app/ante/`, `app/process_proposal.go`, decoders in `pkg/da`, `pkg/inclusion`, `pkg/proof`.

**How to verify:** For every new input field or byte slice, find the code that validates it before first use. Write tests with malformed and extreme values.

## INV-3: Safety holds at the 2/3 threshold

**Statement:** The network is safe only while more than 2/3 of voting power is honest. All code must remain safe with up to 1/3 byzantine voting power and must never assume any single validator, including the proposer, is honest.

**Why:** Logic that trusts a proposer or a minority lets a single byzantine validator corrupt state or split the network.

**Concrete rules:**

- `ProcessProposal` must independently verify every property that `PrepareProposal` established. Never accept a block property because "our own code built it".
- Never treat data as valid because a validator produced or signed it, unless the protocol explicitly makes that signature sufficient.
- New consensus-adjacent logic must be analyzed under the assumption that up to 1/3 of voting power actively misbehaves.

**Where it bites:** `app/process_proposal.go`, `app/prepare_proposal.go`, vote-extension or proposal-injected data.

**How to verify:** For every value taken from a proposed block and used in a state transition, find where it is verified.

## INV-4: No resource amplification

**Statement:** The CPU, memory, and disk consumed by handling any input must be bounded by the gas paid for it or by protocol constants.

**Why:** An amplification vector lets a cheap transaction or block impose large costs on every node, a network-wide DoS.

**Concrete rules:**

- No loop or allocation sized by a user-controlled value without a protocol bound.
- Charge gas before performing the work it pays for, proportional to the actual work, including storage writes and emitted events.
- Watch for quadratic or worse behavior in user-controlled counts (transactions, blobs, namespaces, signers).
- Reason about the worst-case block, not the typical one.

**Where it bites:** `x/*/keeper` message servers, `app/ante/`, square construction and proposal handling.

**How to verify:** Identify every user-controlled size in the diff and confirm a bound or gas charge precedes the work.

## INV-5: No panics on untrusted input

**Statement:** Malformed input must produce an error, never a panic, deadlock, or unbounded allocation.

**Why:** A panic reachable from transaction or block processing halts the node. Input that panics every node halts the chain.

**Concrete rules:**

- No `panic()` or `Must*` functions on untrusted data in transaction or block processing.
- Bounds-check before indexing or slicing data derived from input.
- `recover` is not a fix. Return errors.
- Decoders and parsers must handle arbitrary bytes without crashing.

**Where it bites:** `app/process_proposal.go`, `app/ante/`, `x/*/keeper`, `pkg/da`, `pkg/inclusion`, `pkg/proof`.

**How to verify:** Search the diff for `panic`, `Must`, and unchecked indexing on paths fed by input. Test with malformed bytes.

## INV-6: Wire and encoding changes are explicit decisions

**Statement:** Changes to proto definitions, transaction or blob encoding, or block validity rules are breaking changes that require explicit engineer approval before code is generated.

**Why:** Silent wire breakage splits the network from existing nodes and clients.

**Concrete rules:**

- Flag any change under `proto/` or to encoding logic to the engineer and ask whether breaking is acceptable.
- If breaking is not acceptable, use a compatible alternative: new field, new message, or version gate.
- Never reuse or renumber existing proto field numbers.
- Run `make proto-lint` after proto changes; it includes breaking-change detection.

**Where it bites:** `proto/`, `app/encoding/`, block validity rules.

**How to verify:** Diff review of proto and encoding files plus `make proto-lint`.

## INV-7: Consensus-breaking changes are version-gated

**Statement:** Any change that alters state transition results at existing heights must be gated behind the app version.

**Why:** The multiplexer replays chain history from genesis. An ungated behavior change breaks sync for every new node and forks upgraded nodes from the network.

**Concrete rules:**

- New behavior activates at an app version boundary. See `pkg/appconsts/versioned_consts.go`.
- Ask: "would a node replaying existing blocks compute a different result with this change?" If yes, gate it.
- Migrations run at upgrade boundaries and must themselves be deterministic (INV-1).
- Mark consensus-breaking PRs with `!` in the conventional commit title.

**Where it bites:** `pkg/appconsts/`, `x/*/keeper`, `app/upgrades.go`, anything in the ABCI path.

**How to verify:** For every behavior change, confirm it is either version-gated or provably identical for all existing heights.
