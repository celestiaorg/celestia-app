# Public-Key Cryptography

<!-- toc -->

Consensus-critical data is authenticated using [ECDSA](https://www.secg.org/sec1-v2.pdf) with the curves: [Secp256k1](https://en.bitcoin.it/wiki/Secp256k1) or [Ed25519](https://en.wikipedia.org/wiki/EdDSA#Ed25519).

## Secp256k1

The Secp256k1 key type is used by accounts that submit transactions to be included in Celestia.

### Libraries

A highly-optimized library is available in C (<https://github.com/bitcoin-core/secp256k1>), with wrappers in Go (<https://pkg.go.dev/github.com/ethereum/go-ethereum/crypto/secp256k1>) and Rust (<https://docs.rs/crate/secp256k1>).

### Public-keys

Secp256k1 public keys can be compressed to 256-bits (or 33 bytes) per the format described [here](https://github.com/cosmos/cosmos-sdk/blob/v0.46.15/docs/basics/accounts.md#public-keys).

### Addresses

Cosmos [addresses](https://github.com/cosmos/cosmos-sdk/blob/v0.46.15/docs/basics/accounts.md#addresses) are 20 bytes in length.

### Signatures

<!-- markdown-link-check-disable -->
Deterministic signatures ([RFC-6979](https://tools.ietf.org/rfc/rfc6979.txt)) should be used when signing, but this is not enforced at the protocol level as it cannot be for Secp256k1 signatures.
<!-- markdown-link-check-enable -->

Signatures are represented as the `r` and `s` (each 32 bytes) values of the signature. `r` and `s` take on their usual meaning (see: [SEC 1, 4.1.3 Signing Operation](https://www.secg.org/sec1-v2.pdf)). Signatures are encoded with protobuf as described [here](https://github.com/cosmos/cosmos-sdk/blob/v0.46.15/docs/core/encoding.md).

### Human Readable Encoding

In front-ends addresses are prefixed with the [Bech32](https://en.bitcoin.it/wiki/Bech32) prefix `celestia`. For example, a valid address is `celestia1kj39jkzqlr073t42am9d8pd40tgudc3e2kj9yf`.

## Ed25519

The Ed25519 key type is used by validators.

<!-- markdownlint-disable-next-line MD024 -->
### Libraries

- [crypto/ed25519](https://pkg.go.dev/crypto/ed25519)
- [cometbft crypto/ed25519](https://pkg.go.dev/github.com/cometbft/cometbft@v0.37.0/crypto/ed25519)

### Public Keys

Ed25519 public keys are 32 bytes in length. They often appear in validator configuration files (e.g. `genesis.json`) base64 encoded:

```json
      "pub_key": {
        "type": "tendermint/PubKeyEd25519",
        "value": "DMEMMj1+thrkUCGocbvvKzXeaAtRslvX9MWtB+smuIA="
      }
```

<!-- markdownlint-disable-next-line MD024 -->
### Addresses

Ed25519 addresses are the first 20-bytes of the SHA256 hash of the raw 32-byte public key:

```go
address = SHA256(pubkey)[:20]
```

<!-- markdownlint-disable-next-line MD024 -->
### Signatures

Ed25519 signatures are 64 bytes in length.
