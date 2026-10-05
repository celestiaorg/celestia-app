# Canonical Corto-10 genesis

`genesis.json` is the canonical Corto-10 genesis.

- sha256 `7723f671582b89a7c9da9d8b4ddd9ca48b7efa9b719451346e376dd9d90dc734`
- 626,834 bytes

Verified read-only on 2026-10-05 to be byte-identical to the file every live
validator runs (checked validator-0 and validator-9). Pinned in `GENESIS.sha256`
(`shasum -a 256 -c GENESIS.sha256`).

## The earlier published file

An earlier genesis was published to S3: sha256 `94dd06f9…`, 625,644 bytes. It is
**not** what the network runs. The two files differ only in the ten `genutil`
gentxs:

| | published `94dd06f9` | canonical `7723f671` |
|---|---|---|
| gentx fee | none | 1 utia |
| gentx gas_limit | 200000 (CLI default) | 1000000 |
| gentx signatures | signed for the above | re-signed for the above |

Every other part of the genesis — all module state, bank balances, accounts,
consensus params and the gentx `MsgCreateValidator` bodies — is byte-identical.
The two are **not** semantically equivalent: the differing gentxs yield a
different genesis app hash, so a node must use `7723f671` to join.

This is the two-phase fee-correction redo. `generate.py finalize` signs gentxs
with `--fees 1utia --gas 1000000`, so it reproduces this canonical genesis.

## S3

The S3 copy is the stale `94dd06f9`. Re-publishing the canonical `7723f671` is
recommended so a fresh node does not bootstrap from the wrong genesis. Not done
here.
