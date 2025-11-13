# Permissionless Hyperlane Integration: Fully Automatic System

## Overview

Enable remote chains to **automatically** connect to Celestia without any approval step. The core infrastructure (RoutingISM + canonical tokens) is owned by the **warp module**, making all enrollments permissionless.

This document covers the **two-layer architecture** with automatic enrollment:
1. **ISM Route Enrollment** (Security Layer) - Auto-enroll verified domains
2. **Warp Token Router Enrollment** (Application Layer) - Auto-enroll to module-owned tokens

---

## Architecture: Module-Owned Infrastructure

### Key Concept: Module Ownership

Instead of user/governance ownership, the **warp module itself** owns the infrastructure:

**Current (Owner-Controlled):**
```go
routingISM.Owner = "celestia1governance..."  // Governance owns, manual approval
token.Owner = "celestia1user..."             // User owns, manual approval
```

**Proposed (Module-Owned):**
```go
routingISM.Owner = "celestia1warpmodule..."  // Module owns, automatic enrollment
token.Owner = "celestia1warpmodule..."       // Module owns, automatic enrollment
```

---

## Layer 1: Automatic ISM Route Enrollment

### Domain Registry (Governance-Curated)

The security mechanism is a **governance-curated domain registry**:

```protobuf
message DomainInfo {
  uint32 domain_id = 1;          // Domain ID (e.g., 118)
  string chain_name = 2;         // Human name (e.g., "Osmosis")
  bool verified = 3;             // Governance approved
  string verified_by = 4;        // Who verified (governance)
  int64 verified_at = 5;         // When verified
  string metadata = 6;           // RPC, explorer, docs
}
```

### Modified EnrollISMRoute Logic

```go
func (ms msgServer) EnrollISMRoute(ctx context.Context, msg *types.MsgEnrollISMRoute) (*types.MsgEnrollISMRouteResponse, error) {
    routingISM, err := ms.k.RoutingISMs.Get(ctx, msg.RoutingIsmId.GetInternalId())
    if err != nil {
        return nil, err
    }

    moduleAddr := ms.k.accountKeeper.GetModuleAddress(types.ModuleName)

    // Check ownership model
    if routingISM.Owner != "" && routingISM.Owner != moduleAddr.String() {
        // User-owned RoutingISM - enforce ownership
        if routingISM.Owner != msg.Owner {
            return nil, fmt.Errorf("not routing ism owner")
        }
        // Owner can enroll any ISM for any domain
    } else if routingISM.Owner == moduleAddr.String() {
        // Module-owned RoutingISM - verify domain registry
        domainInfo, err := ms.k.DomainRegistry.Get(ctx, msg.Domain)
        if err != nil || !domainInfo.Verified {
            return nil, fmt.Errorf("domain %d not verified - submit governance proposal", msg.Domain)
        }
        // Anyone can enroll ISM for verified domains
    }

    // Verify ISM exists
    if err := ms.k.AssertIsmExists(ctx, msg.IsmId); err != nil {
        return nil, fmt.Errorf("ISM not found")
    }

    // Check for duplicate
    for _, route := range routingISM.Routes {
        if route.Domain == msg.Domain {
            return nil, fmt.Errorf("route already exists for domain %d", msg.Domain)
        }
    }

    // Add route (automatic for module-owned!)
    routingISM.Routes = append(routingISM.Routes, types.Route{
        Ism:    msg.IsmId,
        Domain: msg.Domain,
    })

    if err := ms.k.RoutingISMs.Set(ctx, msg.RoutingIsmId.GetInternalId(), routingISM); err != nil {
        return nil, err
    }

    _ = ctx.EventManager().EmitTypedEvent(&types.EventEnrollISMRoute{
        RoutingIsmId: msg.RoutingIsmId.String(),
        Domain:       msg.Domain,
        IsmId:        msg.IsmId.String(),
        Enroller:     msg.Owner,
    })

    return &types.MsgEnrollISMRouteResponse{}, nil
}
```

### Domain Verification (Governance)

**Governance Proposal:**
```protobuf
message VerifyDomainProposal {
  string title = 1;
  string description = 2;
  uint32 domain_id = 3;
  string chain_name = 4;
  string metadata = 5;  // JSON with RPC, docs, etc.
}
```

**Workflow:**
```bash
# Submit governance proposal to verify Osmosis
celestia-appd tx gov submit-proposal verify-domain \
  --title "Verify Osmosis (domain 118)" \
  --description "Verify Osmosis domain for permissionless ISM enrollment" \
  --domain-id 118 \
  --chain-name "Osmosis" \
  --metadata '{"rpc":"https://rpc.osmosis.zone","docs":"https://osmosis.zone"}' \
  --from proposer

# Vote and pass
celestia-appd tx gov vote 1 yes --from validator

# Domain is now verified - anyone can enroll ISMs for domain 118
```

---

## Layer 2: Automatic Warp Router Enrollment

### Module-Owned Tokens

**Token Creation:**
```go
func (ms msgServer) CreateCollateralToken(ctx context.Context, msg *types.MsgCreateCollateralToken) (*types.MsgCreateCollateralTokenResponse, error) {
    // ... validation ...

    // Determine owner based on permissionless flag
    tokenOwner := msg.Owner
    if msg.Permissionless {
        // User requested permissionless token - assign to module
        moduleAddr := ms.k.accountKeeper.GetModuleAddress(types.ModuleName)
        tokenOwner = moduleAddr.String()
    }

    newToken := types.HypToken{
        Id:            tokenId,
        Owner:         tokenOwner,  // Module or user
        TokenType:     types.HYP_TOKEN_TYPE_COLLATERAL,
        OriginMailbox: msg.OriginMailbox,
        OriginDenom:   msg.OriginDenom,
    }

    // ...
}
```

**Proto Update:**
```protobuf
message MsgCreateCollateralToken {
  string owner = 1;
  string origin_mailbox = 2;
  string origin_denom = 3;
  bool permissionless = 4;  // NEW: If true, module owns token
}
```

### Modified EnrollRemoteRouter Logic

```go
func (ms msgServer) EnrollRemoteRouter(ctx context.Context, msg *types.MsgEnrollRemoteRouter) (*types.MsgEnrollRemoteRouterResponse, error) {
    token, err := ms.k.HypTokens.Get(ctx, msg.TokenId.GetInternalId())
    if err != nil {
        return nil, fmt.Errorf("token not found")
    }

    moduleAddr := ms.k.accountKeeper.GetModuleAddress(types.ModuleName)

    // Check ownership model
    if token.Owner != "" && token.Owner != moduleAddr.String() {
        // User-owned token - enforce ownership
        if token.Owner != msg.Owner {
            return nil, fmt.Errorf("not token owner")
        }
        // Owner can enroll any router
    } else if token.Owner == moduleAddr.String() {
        // Module-owned token - verify domain registry
        domainInfo, err := ms.k.DomainRegistry.Get(ctx, msg.RemoteRouter.ReceiverDomain)
        if err != nil || !domainInfo.Verified {
            return nil, fmt.Errorf("domain %d not verified", msg.RemoteRouter.ReceiverDomain)
        }
        // Anyone can enroll router for verified domains
    } else {
        // Owner == "" (renounced) - fully permissionless
        // Anyone can enroll any router (caveat emptor)
    }

    // Validate router
    if msg.RemoteRouter == nil || msg.RemoteRouter.ReceiverContract == "" {
        return nil, fmt.Errorf("invalid router")
    }

    // Check duplicate
    exists, _ := ms.k.EnrolledRouters.Has(ctx, collections.Join(msg.TokenId.GetInternalId(), msg.RemoteRouter.ReceiverDomain))
    if exists {
        return nil, fmt.Errorf("router already exists for domain %d", msg.RemoteRouter.ReceiverDomain)
    }

    // Enroll router (automatic for module-owned!)
    if err := ms.k.EnrolledRouters.Set(ctx, collections.Join(msg.TokenId.GetInternalId(), msg.RemoteRouter.ReceiverDomain), *msg.RemoteRouter); err != nil {
        return nil, err
    }

    _ = ctx.EventManager().EmitTypedEvent(&types.EventEnrollRemoteRouter{
        TokenId:          msg.TokenId.String(),
        Owner:            msg.Owner,
        ReceiverDomain:   msg.RemoteRouter.ReceiverDomain,
        ReceiverContract: msg.RemoteRouter.ReceiverContract,
        Gas:              msg.RemoteRouter.Gas,
    })

    return &types.MsgEnrollRemoteRouterResponse{}, nil
}
```

---

## User Experience: Fully Automatic

### One-Time Setup (Governance)

**Verify domains:**
```bash
# Governance verifies Osmosis
celestia-appd tx gov submit-proposal verify-domain --domain-id 118 --chain-name "Osmosis"
celestia-appd tx gov vote 1 yes

# Governance verifies Ethereum
celestia-appd tx gov submit-proposal verify-domain --domain-id 1 --chain-name "Ethereum"
celestia-appd tx gov vote 2 yes

# Now domains 118 and 1 are verified for permissionless enrollment
```

**Create module-owned infrastructure:**
```bash
# Option A: Governance creates canonical RoutingISM
celestia-appd tx gov submit-proposal create-routing-ism \
  --title "Create canonical permissionless RoutingISM" \
  --permissionless true

# Option B: Anyone creates RoutingISM assigned to module
celestia-appd tx ism create-routing-ism --assign-to-module --from deployer

# Create canonical utia token (module-owned)
celestia-appd tx gov submit-proposal create-collateral-token \
  --origin-denom utia \
  --permissionless true
```

### Osmosis Integration (Automatic!)

**Step 1: Osmosis creates their ISM**
```bash
osmosis-appd tx ism create-multisig-ism \
  --validators osmoval1,osmoval2,osmoval3,osmoval4,osmoval5 \
  --threshold 3 \
  --from deployer
# ISM: 0xosmosis_multisig_ism
```

**Step 2: Osmosis enrolls ISM route (automatic!)**
```bash
celestia-appd tx ism enroll-route \
  $CELESTIA_MODULE_ROUTING_ISM \
  118 \
  0xosmosis_multisig_ism \
  --from osmosis-user \
  --node https://rpc.celestia.pops.one

# Output:
# ✅ ISM route enrolled
# Domain 118 → 0xosmosis_multisig_ism
# No approval needed! (domain 118 is verified)
```

**Step 3: Osmosis creates synthetic token**
```bash
osmosis-appd tx warp create-synthetic-token $OSMOSIS_MAILBOX --from deployer
# Token: 0xosmosis_synthetic_utia
```

**Step 4: Osmosis enrolls warp router on their side**
```bash
osmosis-appd tx warp enroll-router \
  0xosmosis_synthetic_utia \
  69420 \
  $CELESTIA_MODULE_UTIA_TOKEN \
  --from deployer
# ✅ Enrolled
```

**Step 5: Osmosis enrolls warp router on Celestia (automatic!)**
```bash
celestia-appd tx warp enroll-router \
  $CELESTIA_MODULE_UTIA_TOKEN \
  118 \
  0xosmosis_synthetic_utia \
  --from osmosis-user \
  --node https://rpc.celestia.pops.one

# Output:
# ✅ Warp router enrolled
# (utia_token, 118) → 0xosmosis_synthetic_utia
# No approval needed! (domain 118 is verified)
```

**Step 6: Transfer assets (works immediately!)**
```bash
# Celestia → Osmosis
celestia-appd tx warp remote-transfer \
  $CELESTIA_MODULE_UTIA_TOKEN \
  118 \
  <osmosis-recipient> \
  1000000utia \
  --from celestia-user \
  --node https://rpc.celestia.pops.one

# ✅ TIA locked
# ✅ Relayer delivers to Osmosis
# ✅ Synthetic TIA minted
# Bridge is OPERATIONAL immediately!
```

---

## Comparison: Proposal vs Automatic

### Proposal + Approval System

```bash
# Osmosis proposes ISM route
celestia> propose-ism-route ... --from osmosis-proposer
# Status: PENDING

# Wait for Celestia owner to approve
celestia> approve-ism-route-proposal ... --from celestia-owner
# ✅ Approved

# Osmosis proposes warp router
celestia> propose-warp-router ... --from osmosis-proposer
# Status: PENDING

# Wait for token owner to approve
celestia> approve-warp-router-proposal ... --from celestia-token-owner
# ✅ Approved

Total time: Hours/days (depends on owner availability)
```

### Fully Automatic System

```bash
# One-time: Governance verifies domain (once per chain)
celestia> gov submit-proposal verify-domain --domain-id 118
celestia> gov vote 1 yes
# ✅ Domain verified

# Osmosis enrolls ISM route (instant!)
celestia> enroll-ism-route ... --from osmosis-user
# ✅ Enrolled

# Osmosis enrolls warp router (instant!)
celestia> enroll-warp-router ... --from osmosis-user
# ✅ Enrolled

Total time: Seconds (after domain is verified)
```

---

## Security: Domain Registry

### Governance Proposal to Verify Domain

**Example proposal:**
```bash
celestia-appd tx gov submit-proposal verify-domain \
  --title "Verify Osmosis Domain 118" \
  --description "Verify Osmosis domain 118 for permissionless enrollment.

Osmosis is a leading Cosmos DEX with:
- 200+ validators
- $500M+ TVL
- Established security track record
- Audited Hyperlane integration

Verifying this domain will allow anyone to:
1. Enroll ISMs with Osmosis validators to Celestia's RoutingISM
2. Enroll warp routers to bridge TIA with Osmosis

This does NOT automatically enroll any routes - it only whitelists the domain for permissionless enrollment." \
  --domain-id 118 \
  --chain-name "Osmosis" \
  --metadata '{
    "rpc": "https://rpc.osmosis.zone",
    "docs": "https://docs.osmosis.zone",
    "explorer": "https://www.mintscan.io/osmosis",
    "github": "https://github.com/osmosis-labs",
    "validators": "https://www.mintscan.io/osmosis/validators"
  }' \
  --deposit 10000000utia \
  --from proposer

# Community reviews and votes
celestia-appd tx gov vote 1 yes --from validator
```

**Verification criteria (what governance should check):**
- ✅ Domain ID matches official chain registry
- ✅ Chain is reputable with established validators
- ✅ Chain has proper security practices
- ✅ Hyperlane integration is audited
- ✅ Team is responsive and legitimate

### Query Verified Domains

```bash
# List all verified domains
celestia-appd query warp domains --verified true

# Output:
# Verified Domains:
#   [1] Domain: 118
#       Chain: Osmosis
#       Verified: true
#       Verified By: celestia1gov...
#       Verified At: Block 1,234,567
#
#   [2] Domain: 1
#       Chain: Ethereum
#       Verified: true
#       Verified By: celestia1gov...
#       Verified At: Block 1,234,890

# Get specific domain
celestia-appd query warp domain 118

# Output:
# Domain Info:
#   Domain ID: 118
#   Chain: Osmosis
#   Verified: true
#   Verified By: celestia1gov...
#   Verified At: Block 1,234,567
#   Metadata:
#     RPC: https://rpc.osmosis.zone
#     Docs: https://docs.osmosis.zone
#     Explorer: https://www.mintscan.io/osmosis
```

---

## Hybrid Model: Both Systems Coexist

Support **both** automatic and approval-based:

### User-Owned Infrastructure
```
RoutingISM owner: celestia1user...
Token owner: celestia1user...

Enrollment: Requires owner approval
Use case: Private/controlled bridges
```

### Module-Owned Infrastructure
```
RoutingISM owner: celestia1warpmodule...
Token owner: celestia1warpmodule...

Enrollment: Automatic (for verified domains)
Use case: Community bridges
```

### Code Logic

```go
if owner == moduleAddr {
    // Module-owned: automatic for verified domains
    if !isDomainVerified(domain) {
        return nil, fmt.Errorf("domain not verified")
    }
    // ✅ Auto-enroll
} else if owner != "" {
    // User-owned: check ownership
    if owner != msg.Owner {
        return nil, fmt.Errorf("not owner")
    }
    // ✅ Owner enrolls
} else {
    // Renounced: fully permissionless
    // ✅ Anyone enrolls
}
```

---

## Implementation Checklist

**Domain Registry:**
- [ ] `DomainInfo` proto definition
- [ ] `VerifyDomainProposal` governance proposal type
- [ ] Keeper methods: `VerifyDomain`, `GetDomain`, `ListDomains`
- [ ] Governance proposal handler
- [ ] Query service

**Module-Owned RoutingISM:**
- [ ] Update `CreateRoutingISM` to support module ownership
- [ ] Update `EnrollISMRoute` to check ownership model
- [ ] Add domain verification check for module-owned
- [ ] Events for automatic enrollments

**Module-Owned Tokens:**
- [ ] Update `CreateCollateralToken` with `permissionless` flag
- [ ] Update `EnrollRemoteRouter` to check ownership model
- [ ] Add domain verification check for module-owned
- [ ] Events for automatic enrollments

**Testing:**
- [ ] Test domain verification proposal
- [ ] Test module-owned RoutingISM automatic enrollment
- [ ] Test module-owned token automatic enrollment
- [ ] Test rejection for unverified domains
- [ ] Test user-owned vs module-owned coexistence

---

## Migration Path

### Phase 1: Deploy Domain Registry
```bash
# Add VerifyDomainProposal to governance
# Deploy domain registry keeper
# Verify initial set of major chains (Osmosis, Ethereum, Arbitrum)
```

### Phase 2: Create Module-Owned Infrastructure
```bash
# Governance creates canonical module-owned RoutingISM
# Governance creates canonical module-owned utia token
```

### Phase 3: Automatic Enrollments Go Live
```bash
# Remote chains enroll ISM routes automatically
# Remote chains enroll warp routers automatically
# No approval needed for verified domains
```

---

## Summary

### Key Differences

| Aspect | Proposal + Approval | Fully Automatic |
|--------|---------------------|-----------------|
| **ISM Routes** | Propose → Approve | Direct enrollment (if domain verified) |
| **Warp Routers** | Propose → Approve | Direct enrollment (if domain verified) |
| **Speed** | Hours/days | Seconds |
| **Governance Role** | Not involved | Verifies domains once |
| **Security** | Owner reviews each | Domain registry guards |
| **Best For** | Private bridges | Community bridges |

### Recommendation

**Deploy both:**
1. **Proposal system** for user-owned infrastructure (privacy/control)
2. **Automatic system** for module-owned infrastructure (speed/scale)

Users choose based on their needs:
- Need control? Use user-owned with proposals
- Need speed? Use module-owned with automatic

---

## References

- Branch: `blasrodri/permissionless-warp-route`
- Repo: `github.com/bcp-innovations/hyperlane-cosmos`
- Proposal System Doc: `permissionless-hyperlane-proposal-approval.md`
