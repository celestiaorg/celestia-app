# End-to-End Integration Guide: Permissionless Hyperlane on Celestia

## Table of Contents

1. [Overview](#overview)
2. [Prerequisites](#prerequisites)
3. [Local Development Setup](#local-development-setup)
4. [Testnet Deployment](#testnet-deployment)
5. [Integration Testing](#integration-testing)
6. [Production Deployment](#production-deployment)
7. [Monitoring & Verification](#monitoring--verification)
8. [Troubleshooting](#troubleshooting)

---

## Overview

This guide walks you through integrating permissionless Hyperlane warp routes on Celestia, from local development to production deployment.

### What You'll Build

- **Module-owned Hyperlane infrastructure** (mailbox, routing ISM, warp tokens)
- **Permissionless warp route enrollment** (anyone can add routes)
- **Cross-chain token transfers** (Celestia ↔ Other chains)

### Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                    Celestia (Domain 69420)                  │
│                                                             │
│  ┌───────────────────────────────────────────────────────┐ │
│  │ Warp Module (celestia1w5pxsp...f2ynfzq8)              │ │
│  │  Owns:                                                 │ │
│  │   • Mailbox (0x...)                                   │ │
│  │   • Routing ISM (0x...)                               │ │
│  │   • Collateral Token for TIA (0x...)                  │ │
│  └───────────────────────────────────────────────────────┘ │
│                           │                                 │
│                           │ Anyone can enroll routes       │
│                           ▼                                 │
│  ┌───────────────────────────────────────────────────────┐ │
│  │ Routes (First-Enrollment-Wins)                        │ │
│  │  • Domain 1 (Ethereum) → 0xeth_synthetic_tia          │ │
│  │  • Domain 118 (Osmosis) → 0xosmo_synthetic_tia        │ │
│  │  • Domain 9999 (Custom) → 0xcustom_synthetic_tia      │ │
│  └───────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────────┘
                           │
                           │ Cross-chain transfers
                           ▼
┌─────────────────────────────────────────────────────────────┐
│              Other Chains (Ethereum, Osmosis, etc)          │
│                 Synthetic TIA Tokens                        │
└─────────────────────────────────────────────────────────────┘
```

---

## Prerequisites

### Required Software

```bash
# Go 1.21+
go version

# Celestia App
cd celestia-app
git checkout blasrodri/permissionless-warp-route

# Build
make build

# Verify
./build/celestia-appd version
```

### Understanding Key Concepts

1. **Module Address**: `celestia1w5pxsp3ag6yllh89xc6jxpr9hfqxhtf2ynfzq8`
   - Deterministic (same everywhere)
   - No private key (pure code)
   - Computed from module name: `SHA256("module" + "warp")[:20]`

2. **Hyperlane Domains**:
   - Celestia Arabica: `69420`
   - Celestia Mainnet: TBD (e.g., `696969`)
   - Ethereum: `1`
   - Osmosis: `118`

3. **Ownership Models**:
   - **Module-owned**: Anyone can enroll routes (permissionless)
   - **User-owned**: Only owner can enroll (traditional)
   - **Ownerless**: Ownership renounced, anyone can enroll

---

## Local Development Setup

### Step 1: Initialize Local Chain

```bash
# Clean state
rm -rf ~/.celestia-app

# Initialize
./build/celestia-appd init local-dev --chain-id test-local

# Create a test account
./build/celestia-appd keys add validator
./build/celestia-appd keys add user1
./build/celestia-appd keys add user2

# Fund accounts in genesis
./build/celestia-appd genesis add-genesis-account \
  $(./build/celestia-appd keys show validator -a) \
  1000000000000utia

./build/celestia-appd genesis add-genesis-account \
  $(./build/celestia-appd keys show user1 -a) \
  1000000000utia

# Create genesis transaction
./build/celestia-appd genesis gentx validator 1000000utia \
  --chain-id test-local

./build/celestia-appd genesis collect-gentxs

# Start chain
./build/celestia-appd start
```

### Step 2: Setup Permissionless Infrastructure

```bash
# In another terminal

# Setup module-owned infrastructure
./build/celestia-appd tx warp setup-permissionless-infrastructure \
  --mode create \
  --local-domain 69420 \
  --origin-denom utia \
  --from validator \
  --chain-id test-local \
  --keyring-backend test \
  --yes

# Wait for transaction to be included
sleep 6

# Query the result - look for mailbox_id, routing_ism_id, token_id in logs
./build/celestia-appd query tx <tx-hash>
```

**Expected Output:**
```json
{
  "code": 0,
  "events": [
    {
      "type": "message",
      "attributes": [
        {"key": "mailbox_id", "value": "0x0000000000000000000000000000000000000000000000000000000000000001"},
        {"key": "routing_ism_id", "value": "0x0000000000000000000000000000000000000000000000000000000000000001"},
        {"key": "token_id", "value": "0x0000000000000000000000000000000000000000000000000000000000000001"}
      ]
    }
  ]
}
```

### Step 3: Test Permissionless Enrollment

```bash
# Save token ID from previous step
TOKEN_ID="0x0000000000000000000000000000000000000000000000000000000000000001"

# User1 enrolls route to Ethereum (domain 1)
./build/celestia-appd tx warp enroll-remote-router \
  --token-id $TOKEN_ID \
  --receiver-domain 1 \
  --receiver-contract 0xETHEREUM_SYNTHETIC_TIA_ADDRESS \
  --gas 100000 \
  --from user1 \
  --chain-id test-local \
  --keyring-backend test \
  --yes

# User2 enrolls route to Osmosis (domain 118)
./build/celestia-appd tx warp enroll-remote-router \
  --token-id $TOKEN_ID \
  --receiver-domain 118 \
  --receiver-contract 0xOSMOSIS_SYNTHETIC_TIA_ADDRESS \
  --gas 100000 \
  --from user2 \
  --chain-id test-local \
  --keyring-backend test \
  --yes

# Verify both routes were enrolled
./build/celestia-appd query warp routers $TOKEN_ID
```

### Step 4: Test First-Enrollment-Wins Protection

```bash
# Try to enroll a different route for Ethereum (should fail)
./build/celestia-appd tx warp enroll-remote-router \
  --token-id $TOKEN_ID \
  --receiver-domain 1 \
  --receiver-contract 0xMALICIOUS_ADDRESS \
  --gas 100000 \
  --from user2 \
  --chain-id test-local \
  --keyring-backend test \
  --yes

# Should see error: "route already enrolled for domain 1 (first-enrollment-wins)"
```

---

## Testnet Deployment

### Step 1: Prepare Deployment Account

```bash
# Create or import deployment key
./build/celestia-appd keys add deployer

# Fund the account (get testnet tokens from faucet)
# Arabica faucet: https://faucet.celestia-arabica-11.com

# Verify balance
./build/celestia-appd query bank balances \
  $(./build/celestia-appd keys show deployer -a) \
  --node https://rpc.celestia-arabica-11.com
```

### Step 2: Deploy Infrastructure

```bash
# Setup module-owned infrastructure on Arabica testnet
./build/celestia-appd tx warp setup-permissionless-infrastructure \
  --mode create \
  --local-domain 69420 \
  --origin-denom utia \
  --from deployer \
  --chain-id arabica-11 \
  --node https://rpc.celestia-arabica-11.com \
  --fees 10000utia \
  --broadcast-mode block \
  --yes

# IMPORTANT: Save the output!
```

**Save These Values:**
```bash
export CELESTIA_MAILBOX_ID="0x..."
export CELESTIA_ROUTING_ISM_ID="0x..."
export CELESTIA_TIA_TOKEN_ID="0x..."
```

### Step 3: Document Infrastructure

Create `deployment-arabica.json`:
```json
{
  "chain": "celestia-arabica-11",
  "domain": 69420,
  "deployed_at": "2025-11-09T15:00:00Z",
  "deployer": "celestia1...",
  "infrastructure": {
    "mailbox_id": "0x...",
    "routing_ism_id": "0x...",
    "warp_token_tia": {
      "id": "0x...",
      "owner": "celestia1w5pxsp3ag6yllh89xc6jxpr9hfqxhtf2ynfzq8",
      "origin_denom": "utia",
      "type": "collateral"
    }
  },
  "routes": []
}
```

### Step 4: Verify Deployment

```bash
# Query mailbox
./build/celestia-appd query hyperlane mailbox $CELESTIA_MAILBOX_ID \
  --node https://rpc.celestia-arabica-11.com

# Expected output:
# owner: celestia1w5pxsp3ag6yllh89xc6jxpr9hfqxhtf2ynfzq8
# local_domain: 69420

# Query routing ISM
./build/celestia-appd query hyperlane ism $CELESTIA_ROUTING_ISM_ID \
  --node https://rpc.celestia-arabica-11.com

# Expected output:
# owner: celestia1w5pxsp3ag6yllh89xc6jxpr9hfqxhtf2ynfzq8
# type: routing
# routes: []

# Query warp token
./build/celestia-appd query warp token $CELESTIA_TIA_TOKEN_ID \
  --node https://rpc.celestia-arabica-11.com

# Expected output:
# owner: celestia1w5pxsp3ag6yllh89xc6jxpr9hfqxhtf2ynfzq8
# origin_denom: utia
# type: collateral
```

---

## Integration Testing

### Test Scenario 1: Enroll Route from Another Chain

Simulate enrolling a route from Ethereum's perspective:

```bash
# Deploy synthetic TIA on Ethereum testnet (Sepolia)
# This would be done using Ethereum tools (Hardhat, Foundry, etc.)

# Example synthetic token address (after deployment)
ETH_SYNTHETIC_TIA="0x1234567890123456789012345678901234567890"

# Enroll the route on Celestia (anyone can do this)
./build/celestia-appd tx warp enroll-remote-router \
  --token-id $CELESTIA_TIA_TOKEN_ID \
  --receiver-domain 1 \
  --receiver-contract $ETH_SYNTHETIC_TIA \
  --gas 200000 \
  --from deployer \
  --chain-id arabica-11 \
  --node https://rpc.celestia-arabica-11.com \
  --fees 5000utia \
  --yes

# Verify route
./build/celestia-appd query warp routers $CELESTIA_TIA_TOKEN_ID \
  --node https://rpc.celestia-arabica-11.com
```

### Test Scenario 2: Multiple Integrators

Different teams can enroll routes independently:

```bash
# Team A enrolls Osmosis route
./build/celestia-appd tx warp enroll-remote-router \
  --token-id $CELESTIA_TIA_TOKEN_ID \
  --receiver-domain 118 \
  --receiver-contract 0xOSMO_SYNTHETIC_TIA \
  --gas 150000 \
  --from team-a-key \
  --chain-id arabica-11 \
  --node https://rpc.celestia-arabica-11.com \
  --fees 5000utia \
  --yes

# Team B enrolls Arbitrum route
./build/celestia-appd tx warp enroll-remote-router \
  --token-id $CELESTIA_TIA_TOKEN_ID \
  --receiver-domain 42161 \
  --receiver-contract 0xARB_SYNTHETIC_TIA \
  --gas 150000 \
  --from team-b-key \
  --chain-id arabica-11 \
  --node https://rpc.celestia-arabica-11.com \
  --fees 5000utia \
  --yes

# Both succeed! Permissionless enrollment
```

### Test Scenario 3: Cross-Chain Transfer

```bash
# Transfer TIA from Celestia to Ethereum
./build/celestia-appd tx warp remote-transfer \
  --token-id $CELESTIA_TIA_TOKEN_ID \
  --amount 1000000utia \
  --receiver-domain 1 \
  --receiver 0xYOUR_ETHEREUM_ADDRESS \
  --from user-key \
  --chain-id arabica-11 \
  --node https://rpc.celestia-arabica-11.com \
  --fees 5000utia \
  --yes

# Monitor the transaction
# The Hyperlane relayer will deliver this to Ethereum
```

---

## Production Deployment

### Step 1: Governance Proposal (Recommended)

For mainnet, use governance to create module-owned infrastructure:

```go
// Submit via CLI or programmatically
./build/celestia-appd tx gov submit-proposal \
  --type MsgSetupPermissionlessInfrastructure \
  --title "Enable Permissionless Warp Routes for TIA" \
  --summary "Creates module-owned Hyperlane infrastructure for permissionless warp route enrollment" \
  --deposit 10000000utia \
  --mode create \
  --local-domain 696969 \
  --origin-denom utia \
  --from proposer-key \
  --chain-id celestia \
  --node https://rpc.celestia.com \
  --yes
```

### Step 2: Community Validation Period

Allow community to review (typically 2-7 days):

```bash
# Query proposal
./build/celestia-appd query gov proposal <proposal-id>

# Vote
./build/celestia-appd tx gov vote <proposal-id> yes \
  --from validator-key \
  --chain-id celestia
```

### Step 3: Post-Deployment Verification

```bash
# After proposal passes, verify infrastructure
./build/celestia-appd query warp token <token-id> --node https://rpc.celestia.com

# Check ownership is module
# owner: celestia1w5pxsp3ag6yllh89xc6jxpr9hfqxhtf2ynfzq8
```

### Step 4: Announce to Ecosystem

Create announcement with:
- **Mailbox ID**
- **Routing ISM ID**
- **TIA Token ID**
- **Integration instructions**

Example announcement:

```markdown
# Permissionless Warp Routes Now Live on Celestia Mainnet

We're excited to announce permissionless warp route enrollment is now available!

## Infrastructure Details

- **Mailbox**: `0x...`
- **Routing ISM**: `0x...`
- **TIA Collateral Token**: `0x...`
- **Owner**: `celestia1w5pxsp3ag6yllh89xc6jxpr9hfqxhtf2ynfzq8` (warp module)

## How to Integrate

1. Deploy synthetic TIA on your chain
2. Enroll route permissionlessly:

```bash
celestia-appd tx warp enroll-remote-router \
  --token-id 0x... \
  --receiver-domain <YOUR_DOMAIN> \
  --receiver-contract <YOUR_SYNTHETIC_TIA> \
  --gas 200000 \
  --from your-key
```

3. Start bridging!

## Security

- **First-enrollment-wins**: Routes cannot be changed after enrollment
- **Module-owned**: No single point of failure
- **Audited**: [link to audit]
```

---

## Monitoring & Verification

### Monitor Route Enrollments

```bash
# Query all routes
./build/celestia-appd query warp routers $TOKEN_ID

# Expected output:
# routes:
#   - domain: 1
#     contract: 0xeth_synthetic_tia
#     gas: 200000
#   - domain: 118
#     contract: 0xosmo_synthetic_tia
#     gas: 150000
```

### Monitor Transfers

```bash
# Query recent warp transfer events
./build/celestia-appd query txs \
  --events 'message.action=/hyperlane.warp.v1.MsgRemoteTransfer' \
  --limit 10

# Check mailbox message count
./build/celestia-appd query hyperlane mailbox-count $MAILBOX_ID
```

### Verify Route Security

```bash
# Attempt duplicate enrollment (should fail)
./build/celestia-appd tx warp enroll-remote-router \
  --token-id $TOKEN_ID \
  --receiver-domain 1 \
  --receiver-contract 0xMALICIOUS \
  --gas 100000 \
  --from attacker-key \
  --yes

# Expected error: "route already enrolled for domain 1"
```

---

## Troubleshooting

### Issue: "current owner mismatch"

**Symptom:**
```
Error: current owner mismatch: expected celestia1abc..., got celestia1xyz...
```

**Solution:**
```bash
# Check actual owner
./build/celestia-appd query warp token $TOKEN_ID

# Use correct owner in --current-owner flag (for transfer mode)
./build/celestia-appd tx warp setup-permissionless-infrastructure \
  --mode transfer \
  --existing-token-id $TOKEN_ID \
  --current-owner $(./build/celestia-appd query warp token $TOKEN_ID | grep owner | awk '{print $2}') \
  --from your-key
```

### Issue: "route already enrolled"

**Symptom:**
```
Error: route already enrolled for domain 118 (first-enrollment-wins)
```

**Solution:**
This is expected behavior! First enrollment wins. To enroll a different domain:

```bash
# Enroll a different domain instead
./build/celestia-appd tx warp enroll-remote-router \
  --token-id $TOKEN_ID \
  --receiver-domain 9999 \
  --receiver-contract 0xNEW_CHAIN \
  --gas 100000 \
  --from your-key
```

### Issue: "insufficient fees"

**Symptom:**
```
Error: insufficient fees; got: 1000utia required: 5000utia
```

**Solution:**
```bash
# Increase fees
./build/celestia-appd tx warp enroll-remote-router \
  ... \
  --fees 10000utia \
  --yes
```

### Issue: Module address unknown

**Question:** How do I find the module address?

**Answer:**
```bash
# Calculate it
python3 -c "
import hashlib
module_name = 'warp'
address = hashlib.sha256(b'module' + module_name.encode()).digest()[:20]
print(f'Hex: 0x{address.hex()}')
"

# Or use keys parse
./build/celestia-appd keys parse 750268063d4689ffdce53635230465ba406bad2a
```

---

## Summary Checklist

### Local Development
- [ ] Built celestia-appd from `blasrodri/permissionless-warp-route`
- [ ] Started local chain
- [ ] Created module-owned infrastructure with `--mode create`
- [ ] Enrolled test routes from multiple users
- [ ] Verified first-enrollment-wins protection

### Testnet
- [ ] Funded deployer account
- [ ] Deployed infrastructure on Arabica
- [ ] Documented mailbox, ISM, and token IDs
- [ ] Enrolled routes from integration partners
- [ ] Tested cross-chain transfers

### Production
- [ ] Submitted governance proposal (or used privileged account)
- [ ] Waited for community approval
- [ ] Verified deployment after activation
- [ ] Announced infrastructure to ecosystem
- [ ] Set up monitoring

### Security
- [ ] Verified module ownership
- [ ] Tested first-enrollment-wins protection
- [ ] Documented security guarantees
- [ ] Set up alerting for unusual activity

---

## Next Steps

1. **Expand to More Chains**: Encourage other chains to deploy synthetic TIA and enroll routes
2. **Relayer Setup**: Deploy Hyperlane relayers to facilitate message delivery
3. **Frontend Integration**: Build UI for easy route enrollment and transfers
4. **Analytics**: Track route enrollment and transfer volume
5. **Documentation**: Create chain-specific integration guides

## Support & Resources

- **Documentation**: `/docs/permissionless-*.md`
- **Code**: `x/warp/msg_server_setup.go`, `x/warp/msg_server_permissionless.go`
- **Tests**: `test/interop/hyperlane_permissionless_simple_test.go`
- **Issues**: Report at [GitHub repo]

---

**🎉 You're now ready to enable permissionless Hyperlane warp routes on Celestia!**
