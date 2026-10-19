# AGENTS.md - Forwarding Module

Agent context for `celestia-app/x/forwarding/`.

## Quick Reference

- **Full specification**: See [README.md](./README.md)
- **Relayer guide**: See [forwarding-relayer docs](https://github.com/celestiaorg/forwarding-relayer/blob/master/RELAYER.md)

## Key Concepts

- **Purpose**: Single-signature cross-chain transfers via Hyperlane warp routes
- **Core invariant**: `derive(destDomain, destRecipient, tokenId[, hookId, hookMetadata]) == forwardAddr` - this IS the authorization
- **Permissionless**: Anyone can trigger forwarding with correct params
- **Hook is committed**: `custom_hook_id` and `custom_hook_metadata` are part of the derivation, so the depositor picks them, not the submitter. Commit to neither and only the default hook works; commit to either and only that exact pair works. A mismatch is `ErrAddressMismatch`.

Address derivation is implemented by `DeriveForwardingAddress` in `types/address.go`.
The hook fields are appended only when the address commits to a hook or metadata;
a missing hookId is written as 32 zero bytes.

## Review Checklist

- [ ] Address derivation matches exactly
- [ ] destRecipient validated as 32 bytes
- [ ] Pre-checks happen BEFORE SendCoins
- [ ] Failed warp transfers return tokens to forwardAddr
- [ ] MsgForward is atomic: a bound token either fully forwards or the tx fails

## Build & Test

```bash
go test ./x/forwarding/... -v
go build ./cmd/celestia-appd
```
