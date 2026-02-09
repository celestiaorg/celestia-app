# Plan: Add v6 -> v7 test case to TestAllUpgrades

## Issue

[#6529](https://github.com/celestiaorg/celestia-app/issues/6529) — The `TestAllUpgrades` test suite covers upgrade paths v2->v3, v3->v4, v4->v5, and v5->v6, but is missing the v6->v7 case. A separate `TestUpgradeLatest` test exists for v6->v7 but it is not part of the comprehensive `TestAllUpgrades` table-driven test.

## File to Modify

`test/docker-e2e/e2e_upgrade_test.go`

## Changes

### Step 1: Add v6->v7 entry to the TestAllUpgrades test table

Add a new entry to the test case slice in `TestAllUpgrades` (after line 78):

```go
{
    baseAppVersion:   6,
    targetAppVersion: 7,
},
```

This is sufficient because `runUpgradeTest` already performs all the generic upgrade validation:

- Starts a chain at `baseAppVersion`
- Tests bank send and PFB submission **before** upgrade
- Signals all validators for the upgrade
- Waits for upgrade height
- Verifies the app version changed to `targetAppVersion`
- Tests bank send and PFB submission **after** upgrade
- Checks validator liveness

### Step 2: Evaluate relationship with TestUpgradeLatest

`TestUpgradeLatest` (line 169) currently uses `appconsts.Version - 1` -> `appconsts.Version` (i.e., v6->v7) and calls `ValidatePreUpgrade`/`ValidatePostUpgrade`/`UpgradeChain`. These methods currently only check the version number — the same thing `runUpgradeTest` already does. So adding v6->v7 to `TestAllUpgrades` makes them overlap.

**Recommendation:** Leave `TestUpgradeLatest` as-is. It serves a different purpose — it's designed to be the place where **version-specific** post-upgrade assertions are added (e.g., validating commission rate changes, zkism module state). Its use of `appconsts.Version` means it automatically tracks the latest version. The `TestAllUpgrades` entry provides regression coverage in the comprehensive sequential upgrade suite.

If desired, `TestUpgradeLatest` could be enhanced separately (outside this issue's scope) to add v7-specific parameter assertions such as:

- Min commission rate changed to 20% (from 10% in v6)
- Validator commission rates updated to >= 20%
- zkism module store is accessible

### Step 3: Build verification

Run `make build` to ensure the change compiles. The actual Docker-based E2E tests (`make test-docker-e2e`) are too heavyweight to run in this context but should be validated in CI.

## Risks and Considerations

- **Low risk**: This is a one-line addition to a test table. The `runUpgradeTest` helper is already battle-tested for v2-v6 upgrades.
- **CI time**: Adding another upgrade test case increases E2E test suite duration. Each upgrade test starts a Docker chain, signals upgrade, and waits for blocks. This is an acceptable tradeoff for completeness.
- **No v7-specific assertions in runUpgradeTest**: The generic test validates version change, bank send, PFB, and liveness — sufficient for the `TestAllUpgrades` purpose. Version-specific checks belong in `TestUpgradeLatest`.
