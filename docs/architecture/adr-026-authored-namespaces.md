# ADR 026: Authored Namespaces

## Changelog

- 2026-03-11: Initial draft (@evan-forbes)

## Status

Proposed

## Abstract

This ADR introduces authored namespaces: a stateless, deterministic form of permissioned namespaces where each account's namespace is derived directly from its signing address. A new namespace version byte is used so that existing v0 namespaces are unaffected.

## Context

Users and protocols building on Celestia often want namespace exclusivity. Having the ability to limit who can post to a namespace is a powerful tool to prevent spam and as we'll see in a later ADR, also for ordering. The straightforward approach is a stateful registry: store an ACL mapping namespaces to allowed signers. This adds state and complexity to Celestia's state machine.

A simpler approach is to make the namespace itself prove authorship. If the namespace ID is derived from the signer's address, then only the holder of that key can produce a valid blob for that namespace.

If a user wants multiple separate permissioned namespaces, they can use sub-namespace indexing (see [Optional: Sub-namespace indexing](#optional-sub-namespace-indexing)) or create additional accounts. If a protocol wants to aggregate blobs from multiple users, readers subscribe to each user's authored namespace rather than a single shared one. The state of "who can post" is managed by the protocol, not by Celestia.

### Goals

- Deterministic, stateless namespace-to-signer binding.
- Zero additional on-chain state.
- Enforced in fibre via a `PromiseRule` (ADR 025). Optionally enforced in the state machine via the existing `ValidateBlobTxSkipCommitment` validation path.

### Non-Goals

- Arbitrary ACL-based namespace permissions (multiple signers per namespace).
- Revocation or transfer of namespace ownership.
- Enforcement for `MsgPayForFibre` via an `AnteDecorator` -- fibre validators enforce this directly via the `PromiseRule` or equivalent.

## Decision

### Namespace version

Introduce a new namespace version byte for authored namespaces. The version signals that the namespace ID embeds the signer's address and must be validated accordingly.

Currently only versions 0 and 255 are supported. Authored namespaces use version 1.

### Namespace layout

Authored namespaces use the standard 29-byte namespace format:

```
[version (1 byte)][address (20 bytes)][padding (8 bytes)]
```

- **Version**: `0x01` (namespace version 1).
- **Address**: the 20-byte Cosmos SDK account address (`sdk.AccAddress`) of the signer, embedded directly.
- **Padding**: 8 zero bytes. Reserved for future use (see [Optional: Sub-namespace indexing](#optional-sub-namespace-indexing)).

### Derivation

Given a signer address `addr []byte` (20 bytes):

```go
func AuthoredNamespace(addr []byte) (Namespace, error) {
    id := make([]byte, NamespaceIDSize) // 28 bytes
    copy(id, addr)                       // first 20 bytes = address
    // remaining 8 bytes are zero (reserved)
    return NewNamespace(NamespaceVersionOne, id)
}
```

### Validation

The authored namespace rule checks that the namespace embedded in the promise (or transaction) was derived from the signer's address:

```go
func ValidateAuthoredNamespace(ns Namespace, signer []byte) error {
    if ns.Version() != NamespaceVersionOne {
        return nil // not an authored namespace, skip
    }
    embedded := ns.ID()[:len(signer)]
    if !bytes.Equal(embedded, signer) {
        return fmt.Errorf("authored namespace signer mismatch: namespace embeds %X, signer is %X", embedded, signer)
    }
    return nil
}
```

Only v1 namespaces are checked. A blob submission may contain a mix of v0 and v1 namespaces -- v0 namespaces pass through without validation, and only v1 namespaces require that the embedded address matches the blob's signer.

### Enforcement: PromiseRule (fibre)

The core validation is wrapped as a `PromiseRule` (ADR 025) for the fibre server and client:

```go
func AuthoredNamespaceRule() PromiseRule {
    return func(ctx context.Context, promise *PaymentPromise) error {
        for _, ns := range promise.Namespaces() {
            if err := ValidateAuthoredNamespace(ns, promise.Signer()); err != nil {
                return err
            }
        }
        return nil
    }
}
```

### Enforcement: BlobTx validation (optional)

If authored namespace enforcement is also desired for `MsgPayForBlobs` submitted directly to the chain (not via fibre), the check slots into the existing `ValidateBlobTxSkipCommitment` function in `x/blob/types/blob_tx.go`. This function already parses the signer and validates signer-namespace relationships -- it runs during both CheckTx and ProcessProposal. The authored namespace check is added alongside the existing `ShareVersionOne` signer check:

```go
signer, err := sdk.AccAddressFromBech32(msgPFB.Signer)
if err != nil {
    return nil, err
}
for _, blob := range bTx.Blobs {
    // existing: share version 1 signer check
    if blob.ShareVersion() == share.ShareVersionOne {
        if !bytes.Equal(blob.Signer(), signer) {
            return nil, ErrInvalidBlobSigner.Wrapf(...)
        }
    }
    // new: authored namespace check
    if err := ValidateAuthoredNamespace(blob.Namespace(), signer); err != nil {
        return nil, err
    }
}
```

No new ante decorator is needed. The signer is already parsed, the blobs are already iterated, and the namespace is already available on each blob.

### Optional: Sub-namespace indexing

The 8 padding bytes after the address can optionally be used as a sub-namespace index, giving each account up to 2^64 distinct authored namespaces without creating additional accounts. The layout becomes:

```
[0x01 (1 byte)][address (20 bytes)][index (8 bytes)]
```

This requires no changes to the validation rule -- the check only compares the first 20 bytes of the ID against the signer. The index bytes are ignored during validation and are free for the user to set.

To use sub-namespace indexing, users simply construct their namespace with a non-zero index:

```go
func AuthoredNamespaceWithIndex(addr []byte, index uint64) (Namespace, error) {
    id := make([]byte, NamespaceIDSize) // 28 bytes
    copy(id, addr)                       // first 20 bytes = address
    binary.BigEndian.PutUint64(id[20:], index)
    return NewNamespace(NamespaceVersionOne, id)
}
```

This is a purely client-side concern. The derivation helper and validation rule do not need to change -- the zero-index `AuthoredNamespace(addr)` call remains the default, and users who want multiple authored namespaces call `AuthoredNamespaceWithIndex` with their chosen index.

### Files changed

| File | Change |
|------|--------|
| `go-square/share/consts.go` | Add `NamespaceVersionOne` constant |
| `go-square/share/namespace.go` | Add `AuthoredNamespace()` constructor, update version validation |
| `fibre/authored_namespace.go` | New. `ValidateAuthoredNamespace` + `AuthoredNamespaceRule` |
| `x/blob/types/blob_tx.go` (optional) | Add `ValidateAuthoredNamespace` call in `ValidateBlobTxSkipCommitment` |

## Consequences

### Positive

- Stateless: no on-chain registry or governance required.
- Simple "human readable" derivation: address bytes are embedded directly
- Backwards compatible: v0 namespaces are unaffected.
- Composable: the core check is a pure function reusable as both a `PromiseRule` and in `ValidateBlobTxSkipCommitment`.
- Protocols choose their own permissioning model -- Celestia provides the primitive.

### Negative

- Readers must subscribe to multiple namespaces to follow multiple authors, rather than a single shared namespace.

## References

- [ADR 025: Standardized Promise Rules Type](adr-025-promise-rule.md) -- `PromiseRule` type and `ChainPromiseRules`
- `go-square/share/namespace.go` -- Namespace type, version constants, constructors
- `go-square/share/consts.go` -- `NamespaceSize`, `NamespaceIDSize`, version definitions
- `x/blob/types/blob_tx.go` -- `ValidateBlobTxSkipCommitment` and existing signer validation
- `fibre/payment_promise.go` -- `PaymentPromise` type
