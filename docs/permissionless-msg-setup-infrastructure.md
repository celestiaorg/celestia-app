# MsgSetupPermissionlessInfrastructure Implementation

## Overview

`MsgSetupPermissionlessInfrastructure` is a Celestia-specific message that enables setting up Hyperlane infrastructure (mailbox, routing ISM, and warp tokens) with module ownership for permissionless enrollment.

## The Module Address

The `warp` module has a deterministic address:

```
Module Name: warp
Hex Address: 0x750268063d4689ffdce53635230465ba406bad2a
Bech32 (Celestia): celestia1w5pxsp3ag6yllh89xc6jxpr9hfqxhtf2ynfzq8
```

You can verify this with:
```bash
./build/celestia-appd keys parse 750268063d4689ffdce53635230465ba406bad2a
```

This address is the SAME across all chains and environments because it's computed deterministically from the module name.

## What Does This Message Do?

### Mode: "create"

Creates new infrastructure owned by the warp module:

1. **Creates a Routing ISM** (empty, ready for permissionless route enrollment)
2. **Creates a Mailbox** with the routing ISM as default
3. **Creates a Collateral Token** for the specified denom
4. **Transfers ownership** of all three to the warp module

**Parameters:**
- `mode`: "create"
- `local_domain`: Hyperlane domain ID (e.g., 69420 for Celestia Arabica)
- `origin_denom`: Native denom to wrap (e.g., "utia")
- `creator`: Your address (just pays gas fees)

### Mode: "transfer"

Transfers existing infrastructure to module ownership:

**Parameters:**
- `mode`: "transfer"
- `existing_mailbox_id`: Mailbox to transfer (optional)
- `existing_routing_ism_id`: Routing ISM to transfer (optional)
- `existing_token_id`: Token to transfer (optional)
- `current_owner`: Your address (must match current ownership)
- `creator`: Your address (just pays gas fees)

## Usage Examples

### Create New Infrastructure

```bash
./build/celestia-appd tx warp setup-permissionless-infrastructure \
  --mode create \
  --local-domain 69420 \
  --origin-denom utia \
  --from my-key \
  --chain-id arabica-11 \
  --node https://rpc.celestia-arabica-11.com
```

This creates:
- A routing ISM owned by `celestia1w5pxsp3ag6yllh89xc6jxpr9hfqxhtf2ynfzq8`
- A mailbox owned by `celestia1w5pxsp3ag6yllh89xc6jxpr9hfqxhtf2ynfzq8`
- A collateral token for `utia` owned by `celestia1w5pxsp3ag6yllh89xc6jxpr9hfqxhtf2ynfzq8`

### Transfer Existing Infrastructure

```bash
# First, create resources normally
./build/celestia-appd tx hyperlane create-routing-ism --from my-key...
# Output: ism_id = 0xabcd...

./build/celestia-appd tx hyperlane create-mailbox --from my-key ...
# Output: mailbox_id = 0x1234...

./build/celestia-appd tx warp create-collateral-token --from my-key...
# Output: token_id = 0x5678...

# Then transfer to module ownership
./build/celestia-appd tx warp setup-permissionless-infrastructure \
  --mode transfer \
  --existing-mailbox-id 0x1234... \
  --existing-routing-ism-id 0xabcd... \
  --existing-token-id 0x5678... \
  --current-owner $(./build/celestia-appd keys show my-key -a) \
  --from my-key \
  --chain-id arabica-11 \
  --node https://rpc.celestia-arabica-11.com
```

## How It Works Internally

### Create Mode

1. Creates routing ISM as `creator`, then transfers ownership to module
2. Creates mailbox as `creator`, then transfers ownership to module
3. Creates collateral token as `creator`, then transfers ownership to module

The implementation uses the existing Hyperlane message servers (`CreateRoutingIsm`, `CreateMailbox`, `CreateCollateralToken`) followed by ownership transfer messages (`UpdateRoutingIsmOwner`, `SetMailbox`, `SetToken`).

### Transfer Mode

Uses the existing ownership transfer messages:
- `UpdateRoutingIsmOwner` for ISMs
- `SetMailbox` for mailboxes
- `SetToken` for tokens

All with `new_owner` set to the warp module address.

## Why Module Ownership Enables Permissionless Enrollment

Once infrastructure is module-owned:

1. **Routing ISM**: Anyone can call `MsgSetRoutingIsmDomain` to add routes (see `x/ism/permissionless_enrollment.go`)
2. **Warp Tokens**: Anyone can call `MsgEnrollRemoteRouter` to add routes (see `x/warp/msg_server_permissionless.go`)
3. **First-enrollment-wins**: The first enrollment for each domain is permanent (prevents route hijacking)

## Files Created

### Protocol Buffers
- `proto/celestia/warp/v1/tx.proto` - Message definition
- `proto/celestia/warp/v1/events.proto` - Event definition (not yet generated)
- `x/warp/types/tx.pb.go` - Generated Go types
- `x/warp/types/codec.go` - Interface registration

### Implementation
- `x/warp/msg_server_setup.go` - Message server implementation
  - `SetupPermissionlessInfrastructure()` - Main handler
  - `createInfrastructure()` - Create mode logic
  - `transferInfrastructure()` - Transfer mode logic
  - Helper functions for ownership transfer

### Module Integration
- `x/warp/module_permissionless.go` - Updated to:
  - Accept `hyperlaneKeeper` parameter
  - Register the new message type
  - Register the new message server

### App Integration
- `app/app.go` - Updated to pass `HyperlaneKeeper` to warp module

## Testing

### Unit/Integration Tests

Use direct keeper access in your integration tests (as you're already doing):

```go
// Create module-owned token directly
moduleAddr := authtypes.NewModuleAddress("warp").String()
msg := &warptypes.MsgCreateCollateralToken{
    Owner:         moduleAddr,
    OriginMailbox: mailboxID,
    OriginDenom:   params.BondDenom,
}
res, err := celestiaApp.WarpKeeper.CreateCollateralToken(ctx, msg)
```

### E2E Tests

Use the `MsgSetupPermissionlessInfrastructure` message:

```bash
# In your devnet setup script
./build/celestia-appd tx warp setup-permissionless-infrastructure \
  --mode create \
  --local-domain 69420 \
  --origin-denom utia \
  --from validator \
  --chain-id test-chain
```

Then test permissionless enrollment:

```bash
# Anyone can enroll routes now
./build/celestia-appd tx warp enroll-remote-router \
  --token-id <module-owned-token-id> \
  --remote-domain 1337 \
  --remote-contract 0x... \
  --from any-user \
  --chain-id test-chain
```

## Complete Flow: Devnet to Production

### 1. Local Development

```bash
# Start devnet
make start-devnet

# Setup permissionless infrastructure
./build/celestia-appd tx warp setup-permissionless-infrastructure \
  --mode create \
  --local-domain 69420 \
  --origin-denom utia \
  --from validator

# Test permissionless enrollment
./build/celestia-appd tx warp enroll-remote-router \
  --token-id <token-id-from-above> \
  --remote-domain 1337 \
  --remote-contract 0xremote... \
  --from user1

# Verify it worked (anyone should be able to enroll)
./build/celestia-appd tx warp enroll-remote-router \
  --token-id <token-id-from-above> \
  --remote-domain 9999 \
  --remote-contract 0xanother... \
  --from user2
```

### 2. Testnet (Arabica)

```bash
# Setup on testnet
./build/celestia-appd tx warp setup-permissionless-infrastructure \
  --mode create \
  --local-domain 69420 \
  --origin-denom utia \
  --from deployer-key \
  --chain-id arabica-11 \
  --node https://rpc.celestia-arabica-11.com \
  --fees 1000utia

# Output will include:
# - mailbox_id: 0x...
# - routing_ism_id: 0x...
# - token_id: 0x...

# Document these IDs for your integration partners
```

### 3. Mainnet

For mainnet, use a **governance proposal** to create module-owned infrastructure:

```go
// Submit governance proposal
proposal := govv1.MsgSubmitProposal{
    Messages: []sdk.Msg{
        &celestiawarptypes.MsgSetupPermissionlessInfrastructure{
            Creator:      govModuleAddr, // gov module submits
            Mode:         "create",
            LocalDomain:  696969, // mainnet domain
            OriginDenom:  "utia",
        },
    },
    Title: "Enable Permissionless Warp Routes for TIA",
    Summary: "Creates module-owned warp infrastructure to allow permissionless warp route enrollment",
}
```

## Security Considerations

### First-Enrollment-Wins Protection

Once a route is enrolled for a domain, it **cannot be changed**:

```go
// In msg_server_permissionless.go
exists, err := keeper.EnrolledRouters.Has(ctx, collections.Join(tokenId, domain))
if exists {
    return error("route already enrolled for domain (first-enrollment-wins)")
}
```

This means:
- ✅ Legitimate chains should enroll immediately after launch
- ✅ Malicious actors cannot overwrite legitimate routes
- ❌ Routes cannot be updated (by design - immutability = security)

### Module Ownership

Module-owned resources have no private key holder, so:
- ✅ No single point of failure
- ✅ No risk of private key compromise
- ✅ Pure permissionless operation
- ❌ Cannot undo mistakes (no owner to fix things)

### Verification Before Transfer

Always verify ownership before transferring to module:

```bash
# Check current owner
./build/celestia-appd query warp token <token-id>

# Ensure it's YOUR address before transferring
./build/celestia-appd tx warp setup-permissionless-infrastructure \
  --mode transfer \
  --existing-token-id <token-id> \
  --current-owner $(./build/celestia-appd keys show my-key -a) \
  --from my-key
```

## Troubleshooting

### "current owner mismatch" error

You specified the wrong `current_owner`. Check actual ownership:
```bash
./build/celestia-appd query warp token <token-id>
./build/celestia-appd query hyperlane mailbox <mailbox-id>
./build/celestia-appd query hyperlane ism <ism-id>
```

### "creator must be signer" error

The `creator` field must match the `--from` flag:
```bash
# Correct
./build/celestia-appd tx warp setup-permissionless-infrastructure \
  --from my-key  # creator will be set to my-key's address

# Wrong - don't try to specify creator explicitly
```

### Events not generated

Event proto types are not yet properly generated. Events are commented out in the implementation with `// TODO` markers.

## Next Steps

1. **Add CLI commands** in `x/warp/client/cli/`
2. **Generate event types** properly (fix proto generation for events.proto)
3. **Add genesis support** for initializing module-owned infrastructure at chain start
4. **Write E2E tests** using the new message
5. **Document integration guide** for other chains wanting to connect

## Related Documentation

- `/docs/permissionless-hyperlane-e2e-setup.md` - E2E setup guide
- `/docs/permissionless-hyperlane-implementation.md` - Implementation details
- `/x/warp/permissionless_enrollment.go` - Permissionless warp route enrollment
- `/x/ism/permissionless_enrollment.go` - Permissionless ISM route enrollment
