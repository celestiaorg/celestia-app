# ADR 15: Namespace ID Size

## Status

Proposed

## Changelog

- 2023/2/17: initial draft
- 2023/2/22: discussion notes
- 2023/2/23: reorder content

## Context

Namespace ID is currently an 8 byte slice. 8 bytes provides a maximum of 2^64 possible namespace IDs. In practice some namespace IDs are reserved for protocol use so the number of namespace IDs available to users is 2^56 - 2. Modifying the size of a namespace ID post-launch is a breaking change and has implications for the NMT, share encoding, etc. so we'd like to carefully consider the size of the namespace ID pre-launch.

## Desirable Criteria

1. A user should be able to randomly generate a namespace that hasn't been used before[^1]
1. There should exist a large enough namespace ID space for all rollups that may exist in the forseeable future (e.g. 100 years)

[^1]: This assumes a user uses sufficient entropy to generate the namespace ID and isn't front-run by an adversary prior to actually using the namespace.

### Criteria 1

The namespace ID must provide at least 72 bits of randomness ([Eager](https://eager.io/blog/how-long-does-an-id-need-to-be/)) to satisfy criteria 1. Since an 8 byte namespace ID can only provide 64 bits of randomness, it fail to meet this criteria.

| Namespace ID size (bytes) | Criteria 1 |
|---------------------------|------------|
| 8                         | ❌          |
| 16                        | ✅          |
| 20                        | ✅          |
| 32                        | ✅          |

Another way to analyze this criteria is to determine the probability of duplicates if there exist N randomly generated namespaces. Columns in the table below represent the approximate probability that a collision would occur if N (e.g. 1 billion) random namespaces are generated. Ref [probability of secure hash collisions](https://www.johndcook.com/blog/2017/01/10/probability-of-secure-hash-collisions/) and [collision calculator](https://kevingal.com/apps/collision.html).

Namespace ID size (bytes) | 1 billion (10^9) | 1 trillion (10^12) | 1 quadrillion (10^15) | 1 quintillion (10^18)
--------------------------|------------------|--------------------|-----------------------|----------------------
8                         | ~0.02674         | 1                  | 1                     | 1
16                        | 0                | ~1.4432e-15        | ~1.4693e-9            | ~0.00147
20                        | 0                | 0                  | 0                     | ~3.4205e-13
32                        | 0                | 0                  | 0                     | 0

> As a rule of thumb, a hash function with range of size N can hash on the order of sqrt(N) values before running into collisions.

Namespace ID size (bytes) | hash funciton range | can hash this many items before running into collision
--------------------------|---------------------|-------------------------------------------------------
8                         | 2^64                | 2^32 = ~4 billion items
16                        | 2^128               | 2^64 = ~1 quintillion items
20                        | 2^160               | 2^80 = ~1 septillion items
32                        | 2^256               | 2^128 = ~3.4 quintillion items

### Criteria 2

We must make some assumptions for the number of rollups that will exist. Ethereum has 223 million unique addresses ([ycharts](https://ycharts.com/indicators/ethereum_cumulative_unique_addresses)) with a yearly growth rate of 18%. If the growth rate remains constant for the next 100 years, Ethereum would have ~4 quadrillion unique addresses ([Google sheet](https://docs.google.com/spreadsheets/d/1vrRM4gAsmC142KrdUI1aCBS5IVFdJeU0q6gwwnM3Ekc/edit?usp=sharing)) which is inconceivably small relative to the total address space 2^160 ([Wolfram Alpha](https://www.wolframalpha.com/input?i=4.05871E%2B15+%2F+2%5E160)). ~4 quadrillion unique addresses is 0.0002% of the 8 byte namespace id space ([Wolfram Alpha](https://www.wolframalpha.com/input?i=4.05871E%2B15+%2F+2%5E160)) so one can assume that any namespace ID size >= 8 bytes will be large enough for all rollups that may exist in the next 100 years.

## Notes

- [SHA256](https://en.wikipedia.org/wiki/SHA-2) has a digest size of 32 bytes so using a namespace ID size of 32 bytes would enable users to generate stable namespace IDs (e.g. `sha256('sov-labs')`) or unique namespace IDs (e.g. `sha256(blob)`) assuming the blob is unique.
- [IPv6](https://en.wikipedia.org/wiki/IPv6) has an address space of 16 bytes and "the address space is deemed large enough for the foreseeable future" ([Wikipedia](https://en.wikipedia.org/wiki/IPv6#Addressing)).
- [UUIDs](https://en.wikipedia.org/wiki/Universally_unique_identifier) have slightly less than 16 bytes of randomness  and are considered "unique enough for practical purposes" ([Towards Data Science](https://towardsdatascience.com/are-uuids-really-unique-57eb80fc2a87)).
- The size of the Ethereum and Bitcoin address space is 2^160 (20 bytes) ([Mastering Ethereum](https://github.com/ethereumbook/ethereumbook/blob/05f0dfe6c41635ac85527a60c06ac5389d8006e7/04keys-addresses.asciidoc) and [Coinhouse](https://www.coinhouse.com/insights/news/what-if-my-wallet-generated-an-existing-bitcoin-address/)).
- The size of Fuel's address space is 32 bytes ([fuel-docs#75](https://github.com/FuelLabs/fuel-docs/issues/75)).

## Tradeoffs

There are some tradeoffs to consider when choosing a namespace ID size. The namespace ID is prefixed to each NMT data leaf. Two namespace IDs are prefixed to each NMT non-leaf hash. Therefore, the nodes of an NMT will be larger based on the namespace ID size. Assuming shares are 512 bytes:

Namespace ID size (bytes) | NMT data leaf size (bytes) | NMT inner node size (bytes)
--------------------------|----------------------------|----------------------------
8                         | 8 + 512 = 520              | 2*8 + 32 = 48
16                        | 16 + 512 = 528             | 2*16 + 32 = 64
20                        | 20 + 512 = 532             | 2*20 + 32 = 72
32                        | 32 + 512 = 544             | 2*32 + 32 = 96

Increasing the size of NMT nodes will increase the size of the NMT proof. Assuming shares are 512 bytes, square size is 128, and the NMT proof is for a single leaf:

Namespace ID size (bytes) | NMT proof size
--------------------------|---------------
8                         | ~336 bytes
16                        | ~448 bytes
20                        | ~504 bytes
32                        | ~672 bytes

Another tradeoff to consider is the size of the namespace ID in the share. Since a share is a fixed 512 bytes, a share's capacity for blob data decreases as the namespace ID increases.

| Namespace ID size (bytes) | Namespace ID size (bytes) / 512 (bytes) |
|---------------------------|-----------------------------------------|
| 8                         | 1.5%                                    |
| 16                        | 3.1%                                    |
| 20                        | 3.9%                                    |
| 32                        | 6.2%                                    |

## Open Questions

1. What are the performance implications on celestia-node for a larger namespace ID size?
1. Is it possible to adopt a large namespace ID size and mitigate the tradeoffs?
    1. It may be possible to avoid writing the namespace ID to continuation blob shares (proposed by @nashqueue)
    1. It may be possible to decrease the bandwidth requirements for NMT proofs by using lossless compression (proposed by @evan-forbes)

## Detailed Design

What changes need to be made to in order to support namespaces of a different length (e.g. 16 bytes)?

- celestia-app
  - [x] Stop using the namespace ID defined by NMT [celestia-app#1385](https://github.com/celestiaorg/celestia-app/pull/1385)
  - [ ] Increase `appconsts.NamespaceSize` to 16 [celestia-app#1419](https://github.com/celestiaorg/celestia-app/pull/1419)
- celestia-core
  - [ ] Modify `TxNamespaceID`
- nmt
  - N/A

## Discussion Notes

- Do we care about collisions created by an adversary?
  - If so, an adversary can look at previously used namespace IDs and perform a woods attack on an existing namespace ID so increasing the namespace ID size doesn't resolve this threat.
  - We should be careful in our documentation to not encourage users to assume that a randomly generated namespace ID is completely unique because:
    - They could have generated a namespace ID without sufficient entropy
    - An adversary can front-run a user's transaction and preemptively post to that namespace ID
- For the non-adversarial use-case, we want to avoid users having to check if a random namespace has already been used.
- Use case for larger namespace ID size: rollups may have multiple namespaces (e.g. Twitter) where a roll-up may give each user a namespace within a namespace range.
- Is it possible to make the namespace ID a parameter, so that the namespace ID is a parameter to proof verification for roll-ups?
  - Assumes that a block height may have a different namespace ID
- There are talks in the Ethereum community about a potential address range increase.
- 20 bytes gives us Ethereum address compatability so Ethereum addresses could be mapped to a Celestia namespace ID.
- Currently namespace ID size = 8 bytes. Each intermediate node in the NMT is 8 (namespace ID) + 8 (namespace ID) + 32 (SHA256)= 48 bytes
- If namespace ID size = 16 bytes. Each intermediate nodes in the NMT become: 16 (namespace ID) + 16 (namespace ID) + 32 (SHA256) = 64 bytes
- Other option: increase size to 32 bytes with an optimization that reserves the first N bytes. The first N bytes wouldn't be sent over the wire.
- Solution to woods attack
  - Rollups can't assume that all blobs in a namespace are honest
  - Rollups shouldn't scan a namespace directly. Instead they should gossip block headers and light clients should only request blobs of interest.

## FLUPs

- [ ] @rootulp explore the possibility of using 32 bytes with an optimization to not send all 32 bytes over the wire.

## Decision

## References

- <https://github.com/celestiaorg/celestia-app/issues/1308>
