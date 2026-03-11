# ADR 025: Standardized Promise Rules Type for Flexible Ordering and Namespace Rules

## Changelog

- 2026-03-11: Initial draft (@evan-forbes)

## Status

Proposed

## Abstract

This ADR introduces a `PromiseRule` function type and `ChainPromiseRules` combinator. The main goal being to add a standardized mechanism for defining specific sets of rules that can be applied to in the Server, Client, and even the celestia-app state machine.

## Context

The fibre server's `UploadShard` handler runs a fixed pipeline:

1. `verifyPromise` -- parse proto to `*PaymentPromise`, validate signature, chain ID, fields, escrow balance
2. `verifyAssignment` -- fetch validator set, compute shard map, check row indices
3. `verifyShard` -- verify rsema1d row inclusion proofs
4. `store.Put` -- persist promise and shard
5. `SignPaymentPromiseValidator` -- sign with validator ed25519 key

The client's `Upload` method runs:

1. `state.Head` -- get validator set
2. `signedPromise` -- construct and sign `PaymentPromise`
3. `valSet.Assign` -- compute shard assignments
4. `uploadShards` -- parallel uploads to validators, collect 2/3+ signatures

All validation logic is wired directly into `UploadShard`. It might be convenient to add routing code to make adding or changes the validation logic dynamic for rolling upgrades and readability. Adding a new concern, such as permissioned namespaces or ordering constraint, presumably would be added via a similar function signature:

```go
func(ctx context.Context, promise *PaymentPromise) error
```

Optionally, we could define this as a specific type to help organize, isolate, and even route in a standard way. This idea is not unlike the cosmos-sdk's `AnteHandler`, albeit less generic.

### Goals

- Rules are self-contained functions that compose via `ChainPromiseRules` without coupling to each other.
- A single `PromiseRule` type is shared between server and client with identical semantics.
- The core validation logic within a rule should be reusable as a Cosmos SDK ante handler. A rule like "namespace X requires signer Y" operates on namespace and signer -- data available in both `*PaymentPromise` and `MsgPayForBlobs`/`MsgPayForFibre`. The same check function should be callable from both a `PromiseRule` and an `AnteDecorator` via thin adapters.
- Zero overhead when no rule is configured (nil check).

### Non-Goals

- Shard-level validation (inspecting row data). Can be added later with a separate `ShardRule` type if needed.
- Configuration via TOML. Rules are function values, set programmatically.

## Alternatives

### A. Direct verification functions, no abstraction

Add each check as a standalone function called directly in `UploadShard` and `Upload`:

```go
func VerifyPermissionedNamespaces(ctx context.Context, promise *PaymentPromise) error { ... }
func VerifyOrdering(ctx context.Context, promise *PaymentPromise) error { ... }
```

Each function is defined in its own file, tested independently, and called inline between `verifyPromise` and `verifyAssignment`:

```go
if err := VerifyPermissionedNamespaces(ctx, promise); err != nil { ... }
if err := VerifyOrdering(ctx, promise); err != nil { ... }
```

The tradeoff is that each new rule requires editing the handler to add the call. There is no native way to compose rules at the config level. When different endpoints handle different message types with different validation requirements, hardcoding each check inline means the handler must branch on message type or each endpoint must duplicate the call-site boilerplate for its specific rule set.

## Decision

### Type definition

New file `fibre/promise_rule.go`:

```go
package fibre

import "context"

// PromiseRule validates a payment promise before the server performs expensive
// shard work (assignment verification, proof verification) and before signing.
// Returning a non-nil error rejects the blob. The promise has already been
// parsed and verified (signature, escrow, chain ID) when the rule runs.
type PromiseRule func(ctx context.Context, promise *PaymentPromise) error

// ChainPromiseRules returns a PromiseRule that runs the given rules in order,
// short-circuiting on the first error.
func ChainPromiseRules(rules ...PromiseRule) PromiseRule {
	return func(ctx context.Context, p *PaymentPromise) error {
		for _, rule := range rules {
			if err := rule(ctx, p); err != nil {
				return err
			}
		}
		return nil
	}
}
```

### Server insertion point

In `server_config.go`, add to `ServerConfig`:

```go
// PromiseRule is an optional rule applied after promise verification but
// before assignment verification, shard verification, storage, and signing.
// If nil, all verified promises are accepted.
PromiseRule PromiseRule `toml:"-"`
```

In `server_upload.go`, insert between `verifyPromise` (line 31) and `verifyAssignment` (line 44). The promise is already parsed and verified at this point:

```go
promise, blobCfg, promiseHash, pruneAt, err := s.verifyPromise(ctx, req.Promise)
if err != nil {
    // ...existing error handling...
}

// apply promise rule before expensive shard work
if s.Config.PromiseRule != nil {
    if err := s.Config.PromiseRule(ctx, promise); err != nil {
        s.log.WarnContext(ctx, "promise rule rejected", "error", err)
        span.RecordError(err)
        span.SetStatus(codes.Error, "promise rule rejected")
        return nil, status.Error(grpccodes.InvalidArgument, fmt.Sprintf("promise rule rejected: %v", err))
    }
    span.AddEvent("promise_rule_passed")
}

log := s.log.With(...)
// ...existing code continues with verifyAssignment...
```

### Client insertion point

In `client_config.go`, add to `ClientConfig`:

```go
// PromiseRule is an optional local rule applied after creating the promise
// but before uploading to validators. If nil, no client-side rule is applied.
PromiseRule PromiseRule
```

In `client_upload.go`, insert between `promise.Hash()` (line 67) and shard assignment (line 73). The promise is already constructed and signed at this point:

```go
promiseHash, err := promise.Hash()
if err != nil {
    // ...existing error handling...
}

// apply promise rule before network I/O
if c.Config.PromiseRule != nil {
    if err := c.Config.PromiseRule(ctx, promise); err != nil {
        span.RecordError(err)
        span.SetStatus(codes.Error, "promise rule rejected")
        return result, fmt.Errorf("fibre: promise rule rejected: %w", err)
    }
    span.AddEvent("promise_rule_passed")
}

span.AddEvent("signed_promise", ...)
// ...existing code continues with Assign...
```

### Files changed

| File | Change |
|------|--------|
| `fibre/promise_rule.go` | New. Type definition + combinator (~15 lines) |
| `fibre/server_config.go` | Add `PromiseRule` field to `ServerConfig` |
| `fibre/server_upload.go` | Insert rule check after `verifyPromise` (~10 lines) |
| `fibre/client_config.go` | Add `PromiseRule` field to `ClientConfig` |
| `fibre/client_upload.go` | Insert rule check after `signedPromise` (~8 lines) |

### Motivating use cases

#### Permissioned namespaces

A permissioned namespace rule restricts a namespace to a single user deterministically. The core check takes a namespace and a signer, then deterministically generates the signer's namespace. If the two do not match an error is thrown.

#### Ordering rules

An ordering rule requires that each new blob in a namespace proves that the previous blob was accepted by a configurable threshold of validator voting power. The core check is parameterized by a threshold fraction and verifies that the previous commitment received sufficient validator signatures. Different deployments choose different thresholds: > 2/3 voting power for standard ordering, > 4/5 for stricter finality guarantees.

## Consequences

### Positive

- Follows the established config pattern (`StoreFn`, `StateClientFn`, `SignerFn`).
- Adding or removing a rule is a one-line change at the composition site.
- The same check logic can be reused in a Cosmos SDK `AnteDecorator` without duplication.

### Negative

- Risk of premature abstraction.

## References

- `fibre/server_upload.go` -- `UploadShard` handler and `verifyPromise`
- `fibre/client_upload.go` -- `Upload` method and `signedPromise`
- `fibre/server_config.go` -- `ServerConfig` struct and existing `Fn` fields
- `fibre/client_config.go` -- `ClientConfig` struct
- `fibre/payment_promise.go` -- `PaymentPromise` type and `Validate()`


