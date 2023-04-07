# Public-Key Cryptography

<!-- toc -->

Consensus-critical data is authenticated using [ECDSA](https://www.secg.org/sec1-v2.pdf), with the curve [secp256k1](https://en.bitcoin.it/wiki/Secp256k1). A highly-optimized library is available in C (<https://github.com/bitcoin-core/secp256k1>), with wrappers in Go (<https://pkg.go.dev/github.com/ethereum/go-ethereum/crypto/secp256k1>) and Rust (<https://docs.rs/crate/secp256k1>).

## Public-keys

Public keys are serialized in a compressed format described [here](https://docs.cosmos.network/v0.46/basics/accounts.html#public-keys).

## Addresses

Celestia supports [secp256k1](https://en.bitcoin.it/wiki/Secp256k1) keys where [addresses](https://docs.cosmos.network/v0.46/basics/accounts.html#addresses) are 20 bytes in length.

### Human Readable Encoding

In front-ends addresses are prefixed with the [Bech32](https://en.bitcoin.it/wiki/Bech32) prefix `celestia`. For example, a valid address is `celestia1kj39jkzqlr073t42am9d8pd40tgudc3e2kj9yf`.

## Signatures

Deterministic signatures ([RFC-6979](https://tools.ietf.org/rfc/rfc6979.txt)) should be used when signing, but this is not enforced at the protocol level as it cannot be.

Signatures are represented as the `r` and `s` (each 32 bytes) values of the signature. `r` and `s` take on their usual meaning (see: [SEC 1, 4.1.3 Signing Operation](https://www.secg.org/sec1-v2.pdf)). Signatures are encoded with protobuf as described [here](https://docs.cosmos.network/v0.46/core/encoding.html#transaction-encoding).
