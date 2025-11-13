# Permissionless Hyperlane Integration - Proposal

## TL;DR

Enable remote chains to connect to Celestia's Hyperlane infrastructure permissionlessly. Two approaches:

1. **Proposal + Approval:** Remote chains propose, Celestia owners approve (secure, controlled)
2. **Pure Permissionless:** Module-owned infrastructure with instant enrollment (fast, open)

Both require **two-layer enrollment**: ISM routes (security) + Warp routes (application).

---

## The Problem

**Current state:** Only owners can enroll connections

**Layer 1 - ISM Routes (Security):**
```
❌ Only RoutingISM owner can add: domain 118 → osmosis_multisig_ism
```

**Layer 2 - Warp Routes (Application):**
```
❌ Only token owner can add: (utia_token, 118) → osmosis_synthetic_utia
```

**Impact:** Remote chains must coordinate off-chain with Celestia owners to connect.

---

## The Architecture: Two Routing Layers

### Layer 1: Routing ISM (Security Layer)

**Purpose:** Determine which ISM validates messages from each domain

```
Celestia RoutingISM:
  domain 118 (Osmosis) → MultisigISM with Osmosis validators
  domain 1 (Ethereum) → MultisigISM with Ethereum validators

Question answered: "Which ISM validates messages from domain 118?"
```

### Layer 2: Warp Routes (Application Layer)

**Purpose:** Map tokens to their remote routers for transfers

```
Celestia utia token:
  (utia_token, 118) → 0xosmosis_synthetic_utia
  (utia_token, 1) → 0xethereum_erc20_utia

Question answered: "Where do tokens go when sent to domain 118?"
```

**Both layers required for a working bridge!**

---

## Solution 1: Proposal + Approval System

### How It Works

```
Remote Chain                    Celestia Owner
     │                               │
     │  ProposeISMRoute              │
     │  ProposeWarpRoute             │
     ├──────────────────────────────>│ (permissionless)
     │  Status: PENDING              │
     │                               │
     │                          ←────┤ Query proposals
     │                               │
     │                          ┌────┤ ApproveISMRoute
     │                          │    │ ApproveWarpRoute
     │                          ▼    │
     ├─→ Bridge OPERATIONAL ✅        │
```

### User Experience

**Osmosis proposes ISM route:**
```bash
# 1. Osmosis creates ISM with their validators
osmosis> create-multisig-ism --validators [...] --threshold 3
# ISM: 0xosmosis_multisig_ism

# 2. Osmosis proposes to Celestia's RoutingISM
celestia> propose-ism-route $CELESTIA_ROUTING_ISM 118 0xosmosis_multisig_ism \
  --metadata '{"validators": [...], "threshold": "3/5", "audit": "https://..."}' \
  --from osmosis-proposer

# ✅ Proposal created: ism-proposal-123
# Status: PENDING
```

**Celestia owner approves:**
```bash
# Query pending proposals
celestia> query ism route-proposals-by-owner celestia1owner... --status pending

# Review proposal details
# - Verify validator set is legitimate
# - Check security audit
# - Test on testnet

# Approve
celestia> approve-ism-route-proposal ism-proposal-123 --from owner

# ✅ Route added: domain 118 → 0xosmosis_multisig_ism
```

**Osmosis proposes warp route:**
```bash
# 1. Osmosis creates synthetic token
osmosis> create-synthetic-token $OSMOSIS_MAILBOX
# Token: 0xosmosis_synthetic_utia

# 2. Osmosis proposes to Celestia token
celestia> propose-warp-route $CELESTIA_UTIA_TOKEN 118 0xosmosis_synthetic_utia \
  --metadata '{"chain": "Osmosis", "docs": "https://...", "testnet_tx": "https://..."}' \
  --from osmosis-proposer

# ✅ Proposal created: warp-proposal-456
# Status: PENDING
```

**Celestia token owner approves:**
```bash
# Query pending proposals
celestia> query warp route-proposals-by-owner celestia1token_owner... --status pending

# Approve
celestia> approve-warp-route-proposal warp-proposal-456 --from token-owner

# ✅ Route enrolled: (utia_token, 118) → 0xosmosis_synthetic_utia
```

**Result:** Celestia ↔ Osmosis bridge operational!

### Key Features

| Feature | Description |
|---------|-------------|
| **Permissionless** | Anyone can propose |
| **Secure** | Owners review before approval |
| **Async** | No real-time coordination |
| **Transparent** | All proposals on-chain |
| **Two-layer** | Separate approval for ISM vs warp |

---

## Solution 2: Pure Permissionless System

### How It Works

**Key concept:** Module owns infrastructure, instant enrollment

```
Ownership:
  RoutingISM.owner = warp_module (not user/governance)
  Token.owner = warp_module (not user)

Enrollment:
  Anyone can enroll any domain instantly
  One transaction, no approval needed
```

**Enrollment logic:**
```go
if routingISM.Owner == warpModule {
    // Module-owned: just enroll!
    routingISM.Routes[msg.Domain] = msg.IsmId
    // ✅ Done - no approval, no verification
}
```

### User Experience

**Osmosis enrolls instantly (one transaction):**
```bash
# 1. Create ISM
osmosis> create-multisig-ism --validators [...] --threshold 3
# ISM: 0xosmosis_multisig_ism

# 2. Enroll ISM route (INSTANT!)
celestia> enroll-ism-route $CELESTIA_MODULE_ROUTING_ISM 118 0xosmosis_multisig_ism \
  --from osmosis-user

# ✅ Route added: domain 118 → 0xosmosis_multisig_ism
# No approval needed!

# 3. Create synthetic token
osmosis> create-synthetic-token $OSMOSIS_MAILBOX
# Token: 0xosmosis_synthetic_utia

# 4. Enroll warp route (INSTANT!)
celestia> enroll-warp-route $CELESTIA_MODULE_UTIA_TOKEN 118 0xosmosis_synthetic_utia \
  --from osmosis-user

# ✅ Route enrolled: (utia_token, 118) → 0xosmosis_synthetic_utia
# No approval needed!

# 5. Transfer assets (works immediately!)
celestia> remote-transfer $CELESTIA_MODULE_UTIA_TOKEN 118 <recipient> 1000000utia

# ✅ Bridge operational instantly!
```

### Key Features

| Feature | Description |
|---------|-------------|
| **Instant** | One transaction, immediate enrollment |
| **Scalable** | Handles unlimited chains |
| **Permissionless** | No approval needed |
| **Simple** | Module-owned infrastructure |
| **Two-layer** | Enrolls both ISM routes + warp routes |

---

## Comparison

| Aspect | Proposal + Approval | Pure Permissionless |
|--------|---------------------|---------------------|
| **Ownership** | User/governance | Warp module |
| **ISM enrollment** | Propose → Approve | Direct (anyone) |
| **Warp enrollment** | Propose → Approve | Direct (anyone) |
| **Speed** | Hours/days | Instant (one tx) |
| **Security** | Owner reviews each proposal | No review |
| **Governance role** | Not involved | Not involved |
| **Best for** | Controlled bridges | Open bridges |
| **Complexity** | High (2 proposal types) | Low (direct enrollment) |

---

## Hybrid Model: Both Coexist

**Support both approaches:**

### User-Owned Infrastructure
```
RoutingISM owner: celestia1user...
Token owner: celestia1user...

Enrollment: Proposal + Approval
Use case: Private/controlled bridges
```

### Module-Owned Infrastructure
```
RoutingISM owner: celestia1warpmodule...
Token owner: celestia1warpmodule...

Enrollment: Instant (anyone can enroll)
Use case: Open community bridges
```

**Users choose based on needs:**
- Need control? → User-owned with proposals
- Need speed? → Module-owned with instant enrollment

---

## Implementation

### New Messages (Proposal System)

**ISM Layer:**
```protobuf
// Anyone can propose
message MsgProposeISMRoute {
  string proposer = 1;
  string routing_ism_id = 2;
  uint32 domain = 3;
  string ism_id = 4;
  string metadata = 5;
}

// Owner approves
message MsgApproveISMRouteProposal {
  string owner = 1;
  string proposal_id = 2;
}
```

**Warp Layer:**
```protobuf
// Anyone can propose
message MsgProposeWarpRoute {
  string proposer = 1;
  string token_id = 2;
  RemoteRouter remote_router = 3;
  string metadata = 4;
}

// Owner approves
message MsgApproveWarpRouteProposal {
  string owner = 1;
  string proposal_id = 2;
}
```

### Logic Changes

**EnrollISMRoute:**
```go
if routingISM.Owner == moduleAddr {
    // Module-owned: just enroll!
    routingISM.Routes[msg.Domain] = msg.IsmId
    // ✅ Instant enrollment
} else if routingISM.Owner != "" {
    // User-owned: check ownership
    if routingISM.Owner != msg.Owner {
        return error("not owner")
    }
    // ✅ Owner enrolls
}
```

**EnrollWarpRoute:**
```go
if token.Owner == moduleAddr {
    // Module-owned: just enroll!
    token.EnrolledRouters[(tokenId, msg.Domain)] = msg.RemoteRouter
    // ✅ Instant enrollment
} else if token.Owner != "" {
    // User-owned: check ownership
    if token.Owner != msg.Owner {
        return error("not owner")
    }
    // ✅ Owner enrolls
}
```

---

## Complete Integration Example

### Osmosis Connects to Celestia

**Setup (both sides need ISM routes):**

| Step | Osmosis | Celestia |
|------|---------|----------|
| **ISM route** | Celestia domain 69420 → celestia_ism | Osmosis domain 118 → osmosis_ism |
| **Warp route** | osmosis_token → celestia_token | celestia_token → osmosis_token |

**With Proposal System:**
```
1. Osmosis proposes ISM route on Celestia → Celestia owner approves
2. Celestia proposes ISM route on Osmosis → Osmosis owner approves
3. Osmosis proposes warp route on Celestia → Celestia token owner approves
4. Osmosis enrolls warp route on Osmosis (direct - they own their token)

Total: 3 proposals + 3 approvals
Time: Hours/days
```

**With Pure Permissionless System:**
```
1. Osmosis enrolls ISM route on Celestia (instant, one tx)
2. Celestia enrolls ISM route on Osmosis (instant, one tx)
3. Osmosis enrolls warp route on Celestia (instant, one tx)
4. Osmosis enrolls warp route on Osmosis (instant, one tx)

Total: 4 transactions, all instant
Time: Seconds
```

---

## Benefits

### Proposal + Approval

**Pros:**
- ✅ Owner maintains full control
- ✅ Review each connection individually
- ✅ Suitable for private/controlled bridges
- ✅ No governance involvement needed

**Cons:**
- ❌ Slower (depends on owner availability)
- ❌ Doesn't scale to hundreds of chains
- ❌ Requires active owner monitoring

### Pure Permissionless

**Pros:**
- ✅ Instant enrollment (one transaction)
- ✅ Scales to unlimited chains
- ✅ Self-service for remote chains
- ✅ No governance needed
- ✅ Simplest possible UX

**Cons:**
- ❌ No review or control
- ❌ Anyone can enroll any domain
- ❌ Module owns infrastructure (not user)

---

## Security Considerations

### Proposal System

**ISM route proposals - what to verify:**
- ✅ ISM exists and has valid configuration
- ✅ Validator set matches known chain validators
- ✅ Threshold is reasonable (e.g., 3-of-5, not 1-of-5)
- ✅ Domain ID matches official registry
- ✅ Security audit exists
- ✅ Test on testnet

**Warp route proposals - what to verify:**
- ✅ Remote router exists and is correct type
- ✅ Remote mailbox matches chain
- ✅ Domain matches ISM route (consistency)
- ✅ Router address is verified
- ✅ Test transfers on testnet

### Pure Permissionless System

**Security implications:**
- ⚠️ No verification of domains
- ⚠️ Anyone can enroll any domain ID
- ⚠️ Malicious actors could enroll fake chains

**Protection mechanisms:**
- First-enrollment-wins (prevent duplicate enrollments)
- On-chain events (monitor all enrollments)
- Module ownership (transparent, no single point of control)
- Off-chain monitoring (relayers, block explorers can flag suspicious enrollments)

---

## Migration Path

### Phase 1: Testnet Deployment
```bash
# Deploy proposal system OR pure permissionless system (or both)
# Test with Arabica testnet
# Validate workflows
```

### Phase 2: Mainnet Rollout
```bash
# Deploy to Celestia mainnet
# Create initial module-owned infrastructure (if pure permissionless)
# Document usage patterns
```

### Phase 3: Adoption
```bash
# Document workflows for remote chains
# Integration guides
# Tooling and dashboards
```

---

## Recommendation

**Deploy both systems:**

1. **Proposal + Approval** for teams who want control
2. **Pure Permissionless** for open community bridges

This gives maximum flexibility:
- Private bridges can use proposals (user-owned)
- Public bridges can use instant enrollment (module-owned)
- Users choose based on their needs

**Priority:** Start with pure permissionless system (simplest, most scalable, addresses biggest pain point)

---

## Implementation Checklist

**Proposal System:**
- [ ] ISM route proposal proto definitions
- [ ] Warp route proposal proto definitions
- [ ] Keeper methods (ProposeISMRoute, ApproveISMRouteProposal, etc.)
- [ ] Query support
- [ ] Tests

**Pure Permissionless System:**
- [ ] Update EnrollISMRoute to check ownership model
- [ ] Update EnrollWarpRoute to check ownership model
- [ ] Module-owned token creation
- [ ] Module-owned RoutingISM creation
- [ ] Tests

**Both:**
- [ ] CLI commands
- [ ] Documentation
- [ ] Testnet deployment
- [ ] Mainnet deployment

---

## References

- **Technical Docs:**
  - Proposal + Approval system: `permissionless-hyperlane-proposal-approval.md`
  - Pure Permissionless system: `permissionless-hyperlane-automatic.md`
- **Branch:** `blasrodri/permissionless-warp-route`
- **Repo:** `github.com/bcp-innovations/hyperlane-cosmos`

---

## Questions for Discussion

1. Which system to implement first? (Both? Proposal? Pure Permissionless?)
2. Should we support both user-owned and module-owned infrastructure?
3. What are the security implications of pure permissionless enrollment?
4. Should there be rate limiting or other safeguards?
5. Migration path for existing integrations?

---

## Summary

**Problem:** Remote chains can't autonomously connect to Celestia's Hyperlane infrastructure

**Solution 1:** Proposal + Approval system (secure, controlled, slower)

**Solution 2:** Pure Permissionless system (instant, open, no approval needed)

**Architecture:** Two layers required:
1. ISM routes (security layer)
2. Warp routes (application layer)

**Recommendation:** Deploy both, let users choose based on needs
