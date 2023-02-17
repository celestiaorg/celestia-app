# ADR 14: Namespace ID Size

## Changelog

- 2023/2/17: initial draft

## Context

Namespace ID is currently an 8 byte slice. 8 bytes provides a maximum of 2^64 possible namespace IDs. In practice some namespace IDs are reserved for protocol use so the number of namespace IDs available to users is less than 2^64. Modifying the size of a namespace ID post-launch has consequences on the NMT, share encoding, etc. so we'd like to carefully consider the size of the namespace ID pre-launch.

Desirandum:

1. Ability to randomly generate a new namespace that hasn't been used before
1.

## Notes

- [SHA256](https://en.wikipedia.org/wiki/SHA-2) has a digest size of 32 bytes so using a namespace ID size of 32 bytes would enable users to generate stable namespace IDs (e.g. `sha256('sov-labs')`) or unique namespace IDs (e.g. `sha256(blob)`) with the SHA256 hash function.
- [IPv6](https://en.wikipedia.org/wiki/IPv6) has an address space of 16 bytes and "the address space is deemed large enough for the foreseeable future" [source](https://en.wikipedia.org/wiki/IPv6#Addressing)
- [UUIDs](https://en.wikipedia.org/wiki/Universally_unique_identifier) have slightly less than 16 bytes of randomnees  and are considered "unique enough for practical purposes" [source](https://towardsdatascience.com/are-uuids-really-unique-57eb80fc2a87)
- The size of Ethereum's private key space is 2^256 (32 bytes) [source](https://github.com/ethereumbook/ethereumbook/blob/develop/04keys-addresses.asciidoc)
- The size of Ethereum's address space is 2^160 (20 bytes) [source](https://github.com/ethereumbook/ethereumbook/blob/05f0dfe6c41635ac85527a60c06ac5389d8006e7/04keys-addresses.asciidoc)
- The size of Bitcoin's address space is 2^160 (20 bytes) [source](https://www.coinhouse.com/insights/news/what-if-my-wallet-generated-an-existing-bitcoin-address/)
- The size of Fuel's contract ID is 32 bytes [source](https://fuellabs.github.io/fuel-specs/master/protocol/id/contract.html)
- The size of Fuel's address space is either 20 bytes [source](https://docs.fuel.sh/v1.1.0/Concepts/Data%20Structures/Addresses.html#addresses) or 32 bytes [question](https://github.com/FuelLabs/fuel-docs/issues/75)

## Options

| Namespace ID size (in bytes) | % of 512 byte share |
|------------------------------|---------------------|
| 8                            | 1.5%                |
| 16                           | 3.1%                |
| 20                           | 3.9%                |
| 32                           | 6.2%                |

## Decision

## Implementation Details

## Status

Proposed

## References

- <https://github.com/celestiaorg/celestia-app/issues/1308>
