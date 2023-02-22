# ADR 15: Namespace ID Size

## Status

Proposed

## Changelog

- 2023/2/17: initial draft

## Context

Namespace ID is currently an 8 byte slice. 8 bytes provides a maximum of 2^64 possible namespace IDs. In practice some namespace IDs are reserved for protocol use so the number of namespace IDs available to users is 2^56 - 2. Modifying the size of a namespace ID post-launch is a breaking change and has implications for the NMT, share encoding, etc. so we'd like to carefully consider the size of the namespace ID pre-launch.

Desirable criteria:

1. Ability to randomly generate a namespace that hasn't been used before
1. Potentially large enough for all rollups that will (ever) exist

## Notes

- The namespace ID must provide at least 72 bits of randomness ([Eager](https://eager.io/blog/how-long-does-an-id-need-to-be/)) to satisfy criteria 1. Since an 8 byte namespace ID can only provide 64 bits of randomness, it fail to meet this criteria.
- [SHA256](https://en.wikipedia.org/wiki/SHA-2) has a digest size of 32 bytes so using a namespace ID size of 32 bytes would enable users to generate stable namespace IDs (e.g. `sha256('sov-labs')`) or unique namespace IDs (e.g. `sha256(blob)`) assuming the blob is unique.
- [IPv6](https://en.wikipedia.org/wiki/IPv6) has an address space of 16 bytes and "the address space is deemed large enough for the foreseeable future" ([Wikipedia](https://en.wikipedia.org/wiki/IPv6#Addressing)).
- [UUIDs](https://en.wikipedia.org/wiki/Universally_unique_identifier) have slightly less than 16 bytes of randomness  and are considered "unique enough for practical purposes" ([Towards Data Science](https://towardsdatascience.com/are-uuids-really-unique-57eb80fc2a87)).
- The size of the Ethereum and Bitcoin address space is 2^160 (20 bytes) ([Mastering Ethereum](https://github.com/ethereumbook/ethereumbook/blob/05f0dfe6c41635ac85527a60c06ac5389d8006e7/04keys-addresses.asciidoc) and [Coinhouse](https://www.coinhouse.com/insights/news/what-if-my-wallet-generated-an-existing-bitcoin-address/)).
- The size of Fuel's contract ID is 32 bytes ([Fuel specs](https://fuellabs.github.io/fuel-specs/master/protocol/id/contract.html)). The size of Fuel's address space is either 20 bytes ([Fuel docs](https://docs.fuel.sh/v1.1.0/Concepts/Data%20Structures/Addresses.html#addresses)) or 32 bytes ([fuel-docs#75](https://github.com/FuelLabs/fuel-docs/issues/75)).

## Options

| Namespace ID size (bytes) | Namespace ID size (bytes) / 512 (bytes) | Ability to randomly generate a namespace that hasn't been used before |
|---------------------------|-----------------------------------------|-----------------------------------------------------------------------|
| 8                         | 1.5%                                    | ❌                                                                     |
| 16                        | 3.1%                                    | ✅                                                                     |
| 20                        | 3.9%                                    | ✅                                                                     |
| 32                        | 6.2%                                    | ✅                                                                     |

## Decision

## Questions

Q: What are the negative consequences of having a large namespace ID size?

A1: The namespace ID is prefixed to each NMT leaf. Two namespace IDs are prefixed to each NMT non-leaf hash. Therefore, the nodes of an NMT will be larger based on the namespace ID size.

A2: The namespace ID is prefixed to each share. Since a share is a fixed 512 bytes, a share's capacity for blob data decreases as the namespace ID increases.

Q: What are the performance implications on celestia-node for a larger namespace ID size?

Q: What is the probability of duplicates if there exist N randomly generated namespaces?

A:

Columns in the table below represent the approximate probability that a collision would occur if N (e.g. 1 billion) random namespaces are generated. Ref [probability of secure hash collisions](https://www.johndcook.com/blog/2017/01/10/probability-of-secure-hash-collisions/) and [collision calculator](https://kevingal.com/apps/collision.html).

Namespace ID size   | 1 billion (10^9) | 1 trillion (10^12) | 1 quadrillion (10^15) | 1 quintillion (10^18)
--------------------|------------------|--------------------|-----------------------|----------------------
8 bytes (64 bits)   | ~0.02674         | 1                  | 1                     | 1
16 bytes (128 bits) | 0                | ~1.4432e-15        | ~1.4693e-9            | ~0.00147
20 bytes (160 bits) | 0                | 0                  | 0                     | ~3.4205e-13
32 bytes (256 bits) | 0                | 0                  | 0                     | 0

> As a rule of thumb, a hash function with range of size N can hash on the order of sqrt(N) values before running into collisions.

In other words

Namespace ID size   | hash funciton range | can hash this many items before running into collision
--------------------|---------------------|----------------------------------------------------------
8 bytes (64 bits)   | 2^64                | 2^32 = ~4 billion items
16 bytes (128 bits) | 2^128               | 2^64 = ~1 quintillion items
20 bytes (160 bits) | 2^160               | 2^80 = ~1 septillion items
32 bytes (256 bits) | 2^256               | 2^128 = ~3.4 quintillion items

## Detailed Design

1. What changes need to be made to celestia-app in order to support namespaces of a different length (e.g. 16 bytes)?
    1. [done] Stop using the namespace ID defined by NMT
    1. Increase `appconsts.NamespaceSize` to 16
1. What changes need to be made to NMT in order to support namespaces of a different length (e.g. 16 bytes)?

## References

- <https://github.com/celestiaorg/celestia-app/issues/1308>
