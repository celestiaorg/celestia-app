# Permissionless Hyperlane Integration: Proposal + Approval System

## Overview

Enable remote chains to **propose** connections to Celestia permissionlessly, with Celestia owners **approving** to maintain security.

This document covers the **two-layer architecture** required for complete Hyperlane integration:
1. **ISM Route Enrollment** (Security Layer) - Validate messages from remote domains
2. **Warp Token Router Enrollment** (Application Layer) - Route asset transfers

---

## Architecture: Two Routing Layers

### Layer 1: Routing ISM (Security Layer)

**Purpose:** Determine which ISM validates messages from each remote domain

**Structure:**
```protobuf
message RoutingISM {
  string id = 1;                  // RoutingISM ID
  string owner = 2;               // Celestia governance/owner
  repeated Route routes = 3;      // domain → ISM mappings
}

message Route {
  string ism = 1;      // ISM that validates messages
  uint32 domain = 2;   // Remote domain ID (e.g., 118 = Osmosis)
}
```

**Example:**
```
Celestia RoutingISM:
  owner: celestia1governance...
  routes:
    - domain 118 → ism: 0xosmosis_multisig_ism (validates Osmosis msgs)
    - domain 1 → ism: 0xethereum_multisig_ism (validates Ethereum msgs)
```

**Current Gatekeeping:** Only RoutingISM owner can add routes

### Layer 2: Warp Token Router (Application Layer)

**Purpose:** Map warp tokens to their remote counterparts for asset transfers

**Structure:**
```go
EnrolledRouters: Map[(tokenId, domain) → RemoteRouter]
```

**Example:**
```
Celestia utia token:
  owner: celestia1token_owner...
  routes:
    - (utia_token, 118) → 0xosmosis_synthetic_utia
    - (utia_token, 1) → 0xethereum_erc20_utia
```

**Current Gatekeeping:** Only token owner can enroll routers

---

## Solution: Two-Step Proposal System (For Both Layers)

### Flow

```
Remote Chain                    Celestia Owner
     │                               │
     │  1. ProposeISMRoute           │
     │  2. ProposeWarpRouter         │
     ├──────────────────────────────>│ (both permissionless)
     │  Status: PENDING              │
     │                               │
     │                          ←────┤ Query pending proposals
     │                               │
     │                          ┌────┤ 1. ApproveISMRoute
     │                          │    │ 2. ApproveWarpRouter
     │                          ▼    │
     ├─→ Bridge OPERATIONAL ✅        │
```

---

## Layer 1: ISM Route Proposals

### New Messages

**1. ProposeISMRoute** (Permissionless)
```protobuf
message MsgProposeISMRoute {
  option (cosmos.msg.v1.signer) = "proposer";

  string proposer = 1;           // Anyone can propose
  string routing_ism_id = 2;     // Celestia's RoutingISM
  uint32 domain = 3;             // Remote domain (e.g., 118)
  string ism_id = 4;             // ISM to validate from this domain
  string metadata = 5;           // Validator set, docs, audit
}
```

**2. ApproveISMRouteProposal** (Owner-only)
```protobuf
message MsgApproveISMRouteProposal {
  option (cosmos.msg.v1.signer) = "owner";

  string owner = 1;              // RoutingISM owner
  string proposal_id = 2;
}
```

**3. RejectISMRouteProposal** (Owner-only)
```protobuf
message MsgRejectISMRouteProposal {
  option (cosmos.msg.v1.signer) = "owner";

  string owner = 1;
  string proposal_id = 2;
  string reason = 3;
}
```

### User Experience

**Osmosis proposes ISM route:**
```bash
# Step 1: Osmosis creates their ISM with their validators
osmosis-appd tx ism create-multisig-ism \
  --validators osmoval1,osmoval2,osmoval3,osmoval4,osmoval5 \
  --threshold 3 \
  --from deployer
# ISM ID: 0xosmosis_multisig_ism

# Step 2: Osmosis proposes route to Celestia
celestia-appd tx ism propose-route \
  $CELESTIA_ROUTING_ISM \
  118 \
  0xosmosis_multisig_ism \
  --metadata '{
    "chain": "Osmosis",
    "validators": ["osmoval1", "osmoval2", "osmoval3", "osmoval4", "osmoval5"],
    "threshold": "3/5",
    "docs": "https://osmosis.zone/celestia-ism",
    "audit": "https://osmosis.zone/audits/ism.pdf"
  }' \
  --from osmosis-proposer \
  --node https://rpc.celestia.pops.one

# Output:
# ✅ ISM route proposal created
# Proposal ID: ism-proposal-123
# Status: PENDING
```

**Celestia owner approves:**
```bash
# Query pending ISM route proposals
celestia-appd query ism route-proposals-by-owner celestia1owner... --status pending

# Output:
# [1] Proposal: ism-proposal-123
#     RoutingISM: 0xcelestia_routing_ism
#     Domain: 118 (Osmosis)
#     ISM: 0xosmosis_multisig_ism
#     Validators: osmoval1, osmoval2, osmoval3, osmoval4, osmoval5
#     Threshold: 3/5

# Verify ISM configuration
celestia-appd query ism multisig-ism 0xosmosis_multisig_ism --node https://rpc.celestia.pops.one

# Approve proposal
celestia-appd tx ism approve-route-proposal ism-proposal-123 \
  --from celestia-routing-ism-owner \
  --node https://rpc.celestia.pops.one

# Output:
# ✅ ISM route approved
# Route added: domain 118 → 0xosmosis_multisig_ism
```

---

## Layer 2: Warp Token Router Proposals

### New Messages

**1. ProposeWarpRouter** (Permissionless)
```protobuf
message MsgProposeWarpRouter {
  option (cosmos.msg.v1.signer) = "proposer";

  string proposer = 1;
  string token_id = 2;            // Celestia token to connect
  RemoteRouter remote_router = 3; // Domain + contract on remote chain
  string metadata = 4;            // Chain, docs, testnet tx
}
```

**2. ApproveWarpRouterProposal** (Owner-only)
```protobuf
message MsgApproveWarpRouterProposal {
  option (cosmos.msg.v1.signer) = "owner";

  string owner = 1;               // Token owner
  string proposal_id = 2;
}
```

**3. RejectWarpRouterProposal** (Owner-only)
```protobuf
message MsgRejectWarpRouterProposal {
  option (cosmos.msg.v1.signer) = "owner";

  string owner = 1;
  string proposal_id = 2;
  string reason = 3;
}
```

### User Experience

**Osmosis proposes warp router:**
```bash
# Osmosis proposes warp route
celestia-appd tx warp propose-router \
  $CELESTIA_UTIA_TOKEN \
  118 \
  $OSMOSIS_SYNTHETIC_UTIA \
  --gas 100000 \
  --metadata '{
    "chain": "Osmosis",
    "docs": "https://osmosis.zone/celestia-bridge",
    "testnet_tx": "https://testnet.osmosis.zone/tx/abc123"
  }' \
  --from osmosis-proposer \
  --node https://rpc.celestia.pops.one

# Output:
# ✅ Warp router proposal created
# Proposal ID: warp-proposal-456
# Status: PENDING
```

**Celestia token owner approves:**
```bash
# Query pending warp router proposals
celestia-appd query warp router-proposals-by-owner celestia1token_owner... --status pending

# Output:
# [1] Proposal: warp-proposal-456
#     Token: 0xcelestia_utia_token
#     Domain: 118 (Osmosis)
#     Contract: 0xosmosis_synthetic_utia

# Approve proposal
celestia-appd tx warp approve-router-proposal warp-proposal-456 \
  --from celestia-token-owner \
  --node https://rpc.celestia.pops.one

# Output:
# ✅ Warp router approved
# Router enrolled: domain 118 → 0xosmosis_synthetic_utia
```

---

## Complete Integration Flow: Osmosis → Celestia

### Phase 1: ISM Routes (Both Directions)

**Osmosis → Celestia:**
```bash
# 1. Osmosis creates ISM
osmosis> create-multisig-ism --validators [...] --threshold 3
# ISM: 0xosmosis_ism

# 2. Osmosis proposes to Celestia's RoutingISM
celestia> propose-ism-route $CELESTIA_ROUTING_ISM 118 0xosmosis_ism --from osmo-proposer
# Proposal: ism-proposal-123

# 3. Celestia approves
celestia> approve-ism-route-proposal ism-proposal-123 --from celestia-owner
# ✅ Route added
```

**Celestia → Osmosis:**
```bash
# 1. Celestia creates ISM
celestia> create-multisig-ism --validators [...] --threshold 2
# ISM: 0xcelestia_ism

# 2. Celestia proposes to Osmosis's RoutingISM
osmosis> propose-ism-route $OSMOSIS_ROUTING_ISM 69420 0xcelestia_ism --from celestia-proposer
# Proposal: ism-proposal-789

# 3. Osmosis approves
osmosis> approve-ism-route-proposal ism-proposal-789 --from osmosis-owner
# ✅ Route added
```

**Result:** Bidirectional ISM routing ✅

### Phase 2: Warp Token Routers (Both Directions)

**Osmosis → Celestia:**
```bash
# 1. Osmosis creates synthetic token
osmosis> create-synthetic-token $OSMOSIS_MAILBOX
# Token: 0xosmosis_synthetic_utia

# 2. Osmosis enrolls their side (direct - they own it)
osmosis> enroll-router 0xosmosis_synthetic_utia 69420 $CELESTIA_UTIA_TOKEN
# ✅ Enrolled

# 3. Osmosis proposes to Celestia token
celestia> propose-warp-router $CELESTIA_UTIA_TOKEN 118 0xosmosis_synthetic_utia --from osmo-proposer
# Proposal: warp-proposal-456

# 4. Celestia token owner approves
celestia> approve-warp-router-proposal warp-proposal-456 --from celestia-token-owner
# ✅ Router enrolled
```

**Result:** Bidirectional warp routing ✅

### Phase 3: Transfer Assets

```bash
# Celestia → Osmosis
celestia> remote-transfer $CELESTIA_UTIA_TOKEN 118 <recipient> 1000000utia
# ✅ TIA locked on Celestia
# ✅ Relayer delivers message to Osmosis
# ✅ Osmosis RoutingISM validates via 0xosmosis_ism
# ✅ Synthetic TIA minted on Osmosis

# Osmosis → Celestia (reverse)
osmosis> remote-transfer 0xosmosis_synthetic_utia 69420 <recipient> 1000000
# ✅ Synthetic TIA burned on Osmosis
# ✅ Relayer delivers to Celestia
# ✅ Celestia RoutingISM validates via 0xcelestia_ism
# ✅ TIA unlocked on Celestia
```

---

## Implementation

### Storage

```go
// ISM Route Proposals
type ISMRouteProposal struct {
    Id           string
    RoutingIsmId HexAddress
    Proposer     string
    Domain       uint32
    IsmId        HexAddress
    Status       ProposalStatus
    CreatedAt    int64
    Metadata     string
}

// Warp Router Proposals
type WarpRouterProposal struct {
    Id           string
    TokenId      HexAddress
    Proposer     string
    RemoteRouter RemoteRouter
    Status       ProposalStatus
    CreatedAt    int64
    Metadata     string
}
```

### Keeper Methods

**ProposeISMRoute:**
```go
func (ms msgServer) ProposeISMRoute(ctx, msg) (*Response, error) {
    // Minimal validation
    if msg.IsmId == "" {
        return nil, fmt.Errorf("invalid ism")
    }

    // Generate proposal ID
    proposalId := generateProposalId(ctx)

    // Create proposal
    proposal := ISMRouteProposal{
        Id:           proposalId,
        RoutingIsmId: msg.RoutingIsmId,
        Proposer:     msg.Proposer,
        Domain:       msg.Domain,
        IsmId:        msg.IsmId,
        Status:       PENDING,
        CreatedAt:    ctx.BlockHeight(),
        Metadata:     msg.Metadata,
    }

    // Store
    k.ISMRouteProposals.Set(ctx, proposalId, proposal)

    // Emit event
    ctx.EventManager().EmitTypedEvent(&EventProposeISMRoute{...})

    return &Response{ProposalId: proposalId}, nil
}
```

**ApproveISMRouteProposal:**
```go
func (ms msgServer) ApproveISMRouteProposal(ctx, msg) (*Response, error) {
    // Get proposal
    proposal := k.ISMRouteProposals.Get(ctx, msg.ProposalId)

    // Get RoutingISM
    routingISM := k.RoutingISMs.Get(ctx, proposal.RoutingIsmId)

    // Verify ownership
    if routingISM.Owner != msg.Owner {
        return nil, fmt.Errorf("not routing ism owner")
    }

    // Check duplicate
    for _, route := range routingISM.Routes {
        if route.Domain == proposal.Domain {
            return nil, fmt.Errorf("route exists for domain %d", proposal.Domain)
        }
    }

    // Add route
    routingISM.Routes = append(routingISM.Routes, Route{
        Ism:    proposal.IsmId,
        Domain: proposal.Domain,
    })
    k.RoutingISMs.Set(ctx, proposal.RoutingIsmId, routingISM)

    // Update proposal
    proposal.Status = APPROVED
    k.ISMRouteProposals.Set(ctx, msg.ProposalId, proposal)

    return &Response{}, nil
}
```

**ProposeWarpRouter & ApproveWarpRouterProposal:** Similar pattern for warp layer

---

## Benefits

| Benefit | Description |
|---------|-------------|
| **Permissionless** | Remote chains propose without coordination |
| **Secure** | Owners review ISM configs and router destinations |
| **Two-Layer Control** | Separate owners for security vs application layer |
| **Async** | No real-time interaction needed |
| **Transparent** | All proposals on-chain and queryable |
| **Auditable** | Complete history of all connection requests |

---

## Security Considerations

### ISM Route Proposals

**What to verify before approving:**
- ✅ ISM exists and has valid configuration
- ✅ Validator set is legitimate (matches known validators for that chain)
- ✅ Threshold is reasonable (e.g., 3-of-5, not 1-of-5)
- ✅ Domain ID matches official chain ID
- ✅ Security audit exists for ISM implementation
- ✅ Test on testnet first

### Warp Router Proposals

**What to verify before approving:**
- ✅ Remote token exists and is correct type (synthetic/collateral)
- ✅ Remote token mailbox matches expected chain
- ✅ Domain ID matches ISM route (consistency)
- ✅ Contract address is verified (not malicious)
- ✅ Test transfers on testnet

---

## Summary

### Two Proposals Required Per Chain

For Osmosis to bridge with Celestia:

**Osmosis submits:**
1. ISM route proposal (security layer)
2. Warp router proposal (application layer)

**Celestia approves:**
1. ISM route proposal (if ISM config is valid)
2. Warp router proposal (if token route is valid)

**Result:** Osmosis ↔ Celestia bridge operational

### Command Summary

```bash
# ISM Layer
celestia-appd tx ism propose-route <routing-ism> <domain> <ism> --from proposer
celestia-appd tx ism approve-route-proposal <proposal-id> --from owner

# Warp Layer
celestia-appd tx warp propose-router <token> <domain> <contract> --from proposer
celestia-appd tx warp approve-router-proposal <proposal-id> --from owner
```

---

## Next Steps

**Implementation:**
- [ ] Fork hyperlane-cosmos
- [ ] Add ISM route proposal proto definitions
- [ ] Add warp router proposal proto definitions
- [ ] Implement keeper methods for both layers
- [ ] Add query support
- [ ] Write tests
- [ ] Deploy to testnet

**Documentation:**
- [ ] User guide for remote chains
- [ ] Owner guide for reviewing proposals
- [ ] Integration examples

**References:**
- Branch: `blasrodri/permissionless-warp-route`
- Repo: `github.com/bcp-innovations/hyperlane-cosmos`
