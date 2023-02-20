# ADR 15: Namespace ID Size

## Changelog

- 2023/2/17: initial draft

## Context

Namespace ID is currently an 8 byte slice. 8 bytes provides a maximum of 2^64 possible namespace IDs. In practice some namespace IDs are reserved for protocol use so the number of namespace IDs available to users is 2^56 - 2. Modifying the size of a namespace ID post-launch is a breaking change and has implications for the NMT, share encoding, etc. so we'd like to carefully consider the size of the namespace ID pre-launch.

Desirandum:

1. Ability to randomly generate a new namespace that hasn't been used before

// what other desirandum do we have?

## Notes

- The namespace ID must provide at least 72 bits of randomness ([Eager](https://eager.io/blog/how-long-does-an-id-need-to-be/)) to satisfy desirandum 1. Since an 8 byte namespace ID can only provide 64 bits of randomness, it fail to meet this desirandum.
- [SHA256](https://en.wikipedia.org/wiki/SHA-2) has a digest size of 32 bytes so using a namespace ID size of 32 bytes would enable users to generate stable namespace IDs (e.g. `sha256('sov-labs')`) or unique namespace IDs (e.g. `sha256(blob)`) with the SHA256 hash function.
- [IPv6](https://en.wikipedia.org/wiki/IPv6) has an address space of 16 bytes and "the address space is deemed large enough for the foreseeable future" ([Wikipedia](https://en.wikipedia.org/wiki/IPv6#Addressing)).
- [UUIDs](https://en.wikipedia.org/wiki/Universally_unique_identifier) have slightly less than 16 bytes of randomnees  and are considered "unique enough for practical purposes" ([Towards Data Science](https://towardsdatascience.com/are-uuids-really-unique-57eb80fc2a87)).
- The size of the Ethereum and Bitcoin address space is 2^160 (20 bytes) ([Mastering Ethereum](https://github.com/ethereumbook/ethereumbook/blob/05f0dfe6c41635ac85527a60c06ac5389d8006e7/04keys-addresses.asciidoc) and [Coinhouse](https://www.coinhouse.com/insights/news/what-if-my-wallet-generated-an-existing-bitcoin-address/)).
- The size of Fuel's contract ID is 32 bytes ([Fuel specs](https://fuellabs.github.io/fuel-specs/master/protocol/id/contract.html)). The size of Fuel's address space is either 20 bytes ([Fuel docs](https://docs.fuel.sh/v1.1.0/Concepts/Data%20Structures/Addresses.html#addresses)) or 32 bytes ([fuel-docs#75](https://github.com/FuelLabs/fuel-docs/issues/75)).

## Options

| Namespace ID size (in bytes) | % of 512 byte share | Desirandum 1 |
|------------------------------|---------------------|--------------|
| 8                            | 1.5%                | ❌            |
| 16                           | 3.1%                | ✅            |
| 20                           | 3.9%                | ✅            |
| 32                           | 6.2%                | ✅            |

## Decision

## Questions

Q: What are the negative consequences of having a large namespace ID size?

A1: The namespace ID is prefixed to each NMT leaf. Two namespace IDs are prefixed to each NMT non-leaf hash. Therefore, the nodes of an NMT will be larger based on the namespace ID size.

A2: The namespace ID is prefixed to each share. Since a share is a fixed 512 bytes, a share's capacity for blob data decreases as the namespace ID increases.

Q: What are the performance implications on celestia-node for a larger namespace ID size?

Q: How can we increase the namespace ID post mainnet?

A1: Construct two data squares and two NMTs. Data square 1 uses NMT 1 with namespace ID size of 8 bytes. Data square 2 uses NMT 2 with a namespace ID size of 32 bytes. Would celestia-nodes sample two separate data squares or is there a clever way to combine both data squares?

## Status

Proposed

## References

- <https://github.com/celestiaorg/celestia-app/issues/1308>
