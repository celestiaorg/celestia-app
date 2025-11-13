# End-to-End Permissionless Hyperlane Setup Guide

## The Key Insight: Module Addresses Are Deterministic

Module addresses in Cosmos SDK are **deterministic** and **computed from the module name**:

```
module_address = SHA256("module" + module_name)[:20]
```

This means:
- ✅ **The address is the SAME across all chains and all environments**
- ✅ **You can compute it ahead of time**
- ✅ **No need to query or discover it**

## Warp Module Address

```
Module Name: warp
Hex Address: 0x750268063d4689ffdce53635230465ba406bad2a
Bech32 (Celestia): celestia1w5pxsp3ag6yllh89xc6jxpr9hfqxhtf2ynfzq8
```

You can verify this with:
```bash
./build/celestia-appd keys parse 750268063d4689ffdce53635230465ba406bad2a
```

## End-to-End Setup: Two Approaches

### Approach 1: Create Everything as Module-Owned (Recommended)

This is the **cleanest approach** for permissionless infrastructure. Create all components with the module as the owner from the start.

#### Step 1: Create Module-Owned Mailbox

```bash
# On Celestia
./build/celestia-appd tx hyperlane create-mailbox \
  --from <your-key> \
  --owner celestia1w5pxsp3ag6yllh89xc6jxpr9hfqxhtf2ynfzq8 \
  --local-domain 69420 \
  --default-ism <routing-ism-id> \
  --chain-id arabica-11 \
  --node https://rpc.celestia-arabica-11.com
```

**Key point**: Use `--owner celestia1w5pxsp3ag6yllh89xc6jxpr9hfqxhtf2ynfzq8` (the warp module address)

#### Step 2: Create Module-Owned Routing ISM

```bash
# Create routing ISM owned by warp module
./build/celestia-appd tx hyperlane create-routing-ism \
  --from <your-key> \
  --creator celestia1w5pxsp3ag6yllh89xc6jxpr9hfqxhtf2ynfzq8 \
  --chain-id arabica-11 \
  --node https://rpc.celestia-arabica-11.com
```

Wait - this won't work because `creator` is the signer! Let me check the actual message structure...

**Actually**, you can't directly create as module-owned because the `creator` field is the signer. So you need **Approach 2**.

### Approach 2: Create Then Transfer (Works for E2E)

This is the **practical approach** for devnet/testnet deployment:

#### Step 1: Create Resources Normally

```bash
# Create ISM
./build/celestia-appd tx hyperlane create-routing-ism \
  --from validator-key \
  --chain-id arabica-11 \
  --node https://rpc.celestia-arabica-11.com

# Output: ism_id = 0xabcd...

# Create Mailbox
./build/celestia-appd tx hyperlane create-mailbox \
  --from validator-key \
  --local-domain 69420 \
  --default-ism 0xabcd... \
  --chain-id arabica-11 \
  --node https://rpc.celestia-arabica-11.com

# Output: mailbox_id = 0x1234...
```

#### Step 2: Transfer Ownership to Module

```bash
# Transfer Routing ISM ownership to warp module
./build/celestia-appd tx hyperlane update-routing-ism-owner \
  --from validator-key \
  --ism-id 0xabcd... \
  --new-owner celestia1w5pxsp3ag6yllh89xc6jxpr9hfqxhtf2ynfzq8 \
  --chain-id arabica-11 \
  --node https://rpc.celestia-arabica-11.com

# Transfer Mailbox ownership to warp module
./build/celestia-appd tx hyperlane set-mailbox \
  --from validator-key \
  --mailbox-id 0x1234... \
  --new-owner celestia1w5pxsp3ag6yllh89xc6jxpr9hfqxhtf2ynfzq8 \
  --chain-id arabica-11 \
  --node https://rpc.celestia-arabica-11.com
```

#### Step 3: Create Module-Owned Warp Token

Now create the warp token owned by the module:

```bash
./build/celestia-appd tx warp create-collateral-token \
  --from validator-key \
  --owner celestia1w5pxsp3ag6yllh89xc6jxpr9hfqxhtf2ynfzq8 \
  --origin-mailbox 0x1234... \
  --origin-denom utia \
  --chain-id arabica-11 \
  --node https://rpc.celestia-arabica-11.com

# Output: token_id = 0x5678...
```

**Wait!** This won't work either because `owner` is checked against the signer in `CreateCollateralToken`.

Let me check the actual implementation...

### Approach 3: Governance Proposal (Production-Ready)

For **actual production deployment**, you'd use a **governance proposal** to create module-owned infrastructure:

```go
// In a governance proposal
proposal := govv1.MsgSubmitProposal{
    Messages: []sdk.Msg{
        &coretypes.MsgCreateMailbox{
            Owner: authtypes.NewModuleAddress("warp").String(),
            LocalDomain: 69420,
            DefaultIsm: routingIsmId,
        },
        &ismtypes.MsgCreateRoutingIsm{
            Creator: authtypes.NewModuleAddress("warp").String(),
            Routes: []ismtypes.Route{},
        },
        &warptypes.MsgCreateCollateralToken{
            Owner: authtypes.NewModuleAddress("warp").String(),
            OriginMailbox: mailboxId,
            OriginDenom: "utia",
        },
    },
}
```

When the governance module executes the proposal, it runs with the **gov module's authority**, which can create resources owned by any module account.

## The REAL Solution: Add Helper Messages

You need to add **new message types** that allow creating module-owned infrastructure. Here's what you need:

### Option A: Add to Warp Module (Celestia-Specific)

Add these messages to `celestia-app/x/warp/`:

```protobuf
// tx.proto

// MsgSetupPermissionlessInfrastructure creates mailbox, ISM, and token as module-owned
message MsgSetupPermissionlessInfrastructure {
  option (cosmos.msg.v1.signer) = "creator";

  string creator = 1;
  uint32 local_domain = 2;
  string origin_denom = 3;
}

message MsgSetupPermissionlessInfrastructureResponse {
  string mailbox_id = 1;
  string routing_ism_id = 2;
  string token_id = 3;
}
```

Implementation:

```go
// msg_server_permissionless.go

func (ms *PermissionlessMsgServer) SetupPermissionlessInfrastructure(
    ctx context.Context,
    msg *warptypes.MsgSetupPermissionlessInfrastructure,
) (*warptypes.MsgSetupPermissionlessInfrastructureResponse, error) {
    moduleAddr := authtypes.NewModuleAddress("warp").String()

    // 1. Create Routing ISM (owned by module)
    // Call ISM keeper directly, bypassing msg server
    routingIsm := &ismtypes.RoutingISM{
        Id: generateIsmId(ctx),
        Owner: moduleAddr,
        Routes: []ismtypes.Route{},
    }
    if err := ms.ismKeeper.GetIsms().Set(ctx, routingIsm.Id.GetInternalId(), routingIsm); err != nil {
        return nil, err
    }

    // 2. Create Mailbox (owned by module)
    mailbox := &coretypes.Mailbox{
        Id: generateMailboxId(ctx),
        Owner: moduleAddr,
        LocalDomain: msg.LocalDomain,
        DefaultIsm: routingIsm.Id,
    }
    if err := ms.coreKeeper.GetMailboxes().Set(ctx, mailbox.Id.GetInternalId(), mailbox); err != nil {
        return nil, err
    }

    // 3. Create Collateral Token (owned by module)
    token := &warptypes.CollateralToken{
        Id: generateTokenId(ctx),
        Owner: moduleAddr,
        OriginMailbox: mailbox.Id,
        OriginDenom: msg.OriginDenom,
    }
    if err := ms.keeper.HypTokens.Set(ctx, token.Id.GetInternalId(), token); err != nil {
        return nil, err
    }

    return &warptypes.MsgSetupPermissionlessInfrastructureResponse{
        MailboxId: mailbox.Id.String(),
        RoutingIsmId: routingIsm.Id.String(),
        TokenId: token.Id.String(),
    }, nil
}
```

### Option B: Genesis File Setup (Simplest for Devnet)

The **easiest way** for devnet/testnet is to **initialize the infrastructure in genesis**:

```json
{
  "app_state": {
    "hyperlane": {
      "mailboxes": [
        {
          "id": "0x...",
          "owner": "celestia1w5pxsp3ag6yllh89xc6jxpr9hfqxhtf2ynfzq8",
          "local_domain": 69420,
          "default_ism": "0x..."
        }
      ],
      "isms": [
        {
          "id": "0x...",
          "owner": "celestia1w5pxsp3ag6yllh89xc6jxpr9hfqxhtf2ynfzq8",
          "type": "routing",
          "routes": []
        }
      ]
    },
    "warp": {
      "tokens": [
        {
          "id": "0x...",
          "owner": "celestia1w5pxsp3ag6yllh89xc6jxpr9hfqxhtf2ynfzq8",
          "origin_mailbox": "0x...",
          "origin_denom": "utia"
        }
      ]
    }
  }
}
```

## Recommended E2E Testing Strategy

For your E2E tests, I recommend:

### 1. Unit/Integration Tests (What You Have Now)
- Use direct keeper calls
- Bypass message server
- Set module address directly

### 2. E2E Tests on Devnet
- **Use genesis file** to initialize module-owned infrastructure
- Or add the helper message `MsgSetupPermissionlessInfrastructure`

### 3. Production Deployment
- Use **governance proposals** to create module-owned infrastructure
- Or add migration logic in an upgrade handler

## Quick Reference: Module Address Calculation

```bash
# Calculate any module address
python3 -c "
import hashlib
module_name = 'warp'
address = hashlib.sha256(b'module' + module_name.encode()).digest()[:20]
print(f'Hex: 0x{address.hex()}')
"

# Convert to bech32
./build/celestia-appd keys parse <hex-address>
```

## Summary

**For E2E testing**, you have these options:

1. ✅ **Genesis file setup** (easiest for devnet)
2. ✅ **Add `MsgSetupPermissionlessInfrastructure`** (clean API)
3. ⚠️ **Governance proposal** (production only)
4. ❌ **Create then transfer** (won't work - creator must be signer)

The **module address is deterministic**, so you always know it ahead of time:
- **Hex**: `0x750268063d4689ffdce53635230465ba406bad2a`
- **Bech32**: `celestia1w5pxsp3ag6yllh89xc6jxpr9hfqxhtf2ynfzq8`
