# `x/consensustimeouts`

## Abstract

The `x/consensustimeouts` module owns the eight consensus timeout values that the app returns to CometBFT in `abci.ResponseFinalizeBlock.TimeoutInfo`. Governance updates them via `MsgUpdateParams` on `celestia.consensustimeouts.v1.Msg`. The module was introduced in app version 9 as part of CIP-048; its defaults come from `pkg/appconsts/initial_consts.go`.

## Parameters

| Param                     | What it controls                                                                | Bounds         | Default  |
|---------------------------|---------------------------------------------------------------------------------|----------------|----------|
| `TimeoutPropose`          | How long a validator waits to receive a block proposal in a given round.        | [500 ms, 10 s] | 3000 ms  |
| `TimeoutProposeDelta`     | Increment added to `TimeoutPropose` per round (handles slow proposers).         | [100 ms, 10 s] |  500 ms  |
| `TimeoutPrevote`          | How long a validator waits for 2/3+ prevotes after a round step begins.         | [500 ms, 10 s] | 2000 ms  |
| `TimeoutPrevoteDelta`     | Increment added to `TimeoutPrevote` per round.                                  | [100 ms, 10 s] |  500 ms  |
| `TimeoutPrecommit`        | How long a validator waits for 2/3+ precommits.                                 | [500 ms, 10 s] | 3000 ms  |
| `TimeoutPrecommitDelta`   | Increment added to `TimeoutPrecommit` per round.                                | [100 ms, 10 s] |  500 ms  |
| `TimeoutCommit`           | Time between finalizing a block and starting the next round. Drives block time. | [  1 ms,  2 s] |  500 ms  |
| `DelayedPrecommitTimeout` | Primary driver of expected block time (paired with `TimeoutCommit`).            | [100 ms, 10 s] | 2100 ms  |

> Block time `~=` `DelayedPrecommitTimeout + TimeoutCommit` (~2.6 s under CIP-048 defaults).

Bounds are enforced by `Params.Validate` (see [`types/params.go`](./types/params.go)). `MsgUpdateParams` requires all eight fields to be supplied on every update.

## Block-time dependents

> **Warning:** changing `TimeoutCommit` or `DelayedPrecommitTimeout` changes the wall-clock duration of every block-count-denominated parameter on chain. Any governance proposal that retunes block time MUST include rescaled values for the dependents below, or the chain will silently change the meaning of evidence windows, signing windows, and IBC client liveness thresholds.

| Dependent                     | Owner module      | Message                            |
|-------------------------------|-------------------|------------------------------------|
| `Evidence.MaxAgeNumBlocks`    | SDK `x/consensus` | `MsgUpdateParams` on `x/consensus` |
| `Evidence.MaxAgeDuration`     | SDK `x/consensus` | same                               |
| IBC `MaxExpectedTimePerBlock` | IBC `connection`  | gov-controlled IBC params          |
| `SignedBlocksWindow`          | SDK `x/slashing`  | `MsgUpdateParams` on `x/slashing`  |
| `MinSignedPerWindow`          | SDK `x/slashing`  | same                               |

Out of scope (not currently governance-changeable): the per-chain upgrade height delays in [`pkg/appconsts/initial_consts.go`](../../pkg/appconsts/initial_consts.go) for Arabica, Mocha, and Mainnet. Those are baked at compile time and must be changed via an app upgrade.

## Rescaling rule

If `newBlockTime` differs from `oldBlockTime`, every block-count-denominated parameter `c` should be rescaled to:

```text
c' = c * oldBlockTime / newBlockTime
```

so that its wall-clock meaning is preserved.

Time-denominated dependents (`Evidence.MaxAgeDuration`, IBC `MaxExpectedTimePerBlock`) are not rescaled by block time per se; they are reset to whatever wall-clock window the network still wants to enforce. In practice they stay constant when the goal is "same wall-clock window, faster blocks".

**Worked example.** Halving block time from 2.6 s to 1.3 s doubles `MaxAgeNumBlocks` from `559_940` to `~1_119_880`, preserving the ~16.85-day evidence window from CIP-037 / CIP-048. `MaxAgeDuration` itself stays at `337h`. `SignedBlocksWindow` is rescaled the same way (e.g. `10_000` -> `20_000`). IBC `MaxExpectedTimePerBlock` stays at its wall-clock target (typically `5 * newBlockTime` worth of nanoseconds; halving block time halves it).

## Worked example: retuning from 2.6 s to 1.3 s

The proposal below is a single `cosmos.gov.v1.MsgSubmitProposal` carrying four messages: one to retune the consensus timeouts, and three to rescale the block-time dependents.

```json
{
  "messages": [
    {
      "@type": "/celestia.consensustimeouts.v1.MsgUpdateParams",
      "authority": "celestia10d07y265gmmuvt4z0w9aw880jnsr700jtgz4v7",
      "params": {
        "timeout_propose": "1500ms",
        "timeout_propose_delta": "250ms",
        "timeout_prevote": "1000ms",
        "timeout_prevote_delta": "250ms",
        "timeout_precommit": "1500ms",
        "timeout_precommit_delta": "250ms",
        "timeout_commit": "250ms",
        "delayed_precommit_timeout": "1050ms"
      }
    },
    {
      "@type": "/cosmos.consensus.v1.MsgUpdateParams",
      "authority": "celestia10d07y265gmmuvt4z0w9aw880jnsr700jtgz4v7",
      "block": {
        "max_bytes": "33554432",
        "max_gas": "-1"
      },
      "evidence": {
        "max_age_num_blocks": "1119880",
        "max_age_duration": "1213200s",
        "max_bytes": "1048576"
      },
      "validator": {
        "pub_key_types": ["ed25519"]
      },
      "abci": {
        "vote_extensions_enable_height": "0"
      }
    },
    {
      "@type": "/ibc.core.connection.v1.MsgUpdateParams",
      "signer": "celestia10d07y265gmmuvt4z0w9aw880jnsr700jtgz4v7",
      "params": {
        "max_expected_time_per_block": "6500000000"
      }
    },
    {
      "@type": "/cosmos.slashing.v1beta1.MsgUpdateParams",
      "authority": "celestia10d07y265gmmuvt4z0w9aw880jnsr700jtgz4v7",
      "params": {
        "signed_blocks_window": "20000",
        "min_signed_per_window": "0.750000000000000000",
        "downtime_jail_duration": "60s",
        "slash_fraction_double_sign": "0.020000000000000000",
        "slash_fraction_downtime": "0.000000000000000000"
      }
    }
  ],
  "metadata": "ipfs://...",
  "deposit": "10000000000utia",
  "title": "Halve block time to 1.3s",
  "summary": "Retune consensus timeouts to target ~1.3s block time and rescale all block-count-denominated parameters to preserve their wall-clock meaning."
}
```

Submit it with:

```shell
celestia-appd tx gov submit-proposal proposal.json \
  --from <key> \
  --chain-id celestia \
  --gas auto \
  --gas-adjustment 1.3 \
  --fees 5000utia
```

Notes:

- The authority above is the standard `x/gov` module account on celestia. Replace it with the value returned by `celestia-appd query auth module-account gov` for the target chain.
- `MaxAgeDuration` is given in seconds (`1213200s = 337h`); it is not rescaled by block time and stays at the wall-clock target.
- IBC `max_expected_time_per_block` is in nanoseconds. `6_500_000_000 ns = 6.5 s` matches `5 * 1.3 s` per the CIP-048 sizing of `MaxExpectedTimePerBlock`.
- The non-time slashing fields above are illustrative; in a real proposal they should match the current chain values for any field you do not intend to change. Query them first via `celestia-appd query slashing params`.

## CLI override

`celestia-appd start` accepts `--timeout-commit` and `--delayed-precommit-timeout` flags, defined in [`cmd/celestia-appd/cmd/root.go`](../../cmd/celestia-appd/cmd/root.go). These flags are honored only on chain-IDs OTHER than `celestia`, `mocha-4`, and `arabica-11`. They are intended for local devnets and the e2e harness, never for production chains. On the three canonical chain-IDs the values come from on-chain `x/consensustimeouts` params and the flags are ignored.

## Resources

1. <https://github.com/celestiaorg/CIPs/blob/main/cips/cip-048.md>
2. <https://github.com/celestiaorg/CIPs/blob/main/cips/cip-037.md>
