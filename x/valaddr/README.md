# `x/valaddr`

## Abstract

The `x/valaddr` module is an on-chain registry that maps a validator's consensus address to the network host of the [fibre](../../specs/src/fibre.md) data availability server that validator operates. Fibre clients read this registry to discover which host to dial for each validator in the active set; a validator whose fibre server is not registered here receives no fibre traffic, because clients have no way to find it.

The module is available starting from app version 10.

## State

The module stores one record per validator, keyed by the validator's consensus address:

```proto
// FibreProviderInfo contains the service details for a fibre provider.
message FibreProviderInfo {
  // host is the network address for the fibre service provider
  string host = 1;
}
```

Entries are garbage-collected in `EndBlock`: a record is deleted once its validator has either been removed from staking state entirely, or has been jailed and unbonded for longer than a 7-day grace period (`JailedGracePeriod`). A re-registered validator simply submits a new `MsgSetFibreProviderInfo`.

The module has no parameters and an empty genesis state — the registry is populated exclusively through transactions.

## Messages

### `MsgSetFibreProviderInfo`

Sets or updates the fibre host for a validator.

```proto
// MsgSetFibreProviderInfo allows a validator to set or update their fibre provider information.
message MsgSetFibreProviderInfo {
  option (cosmos.msg.v1.signer) = "signer";
  // signer is the validator's operator address (celestiavaloper...)
  string signer = 1 [(cosmos_proto.scalar) = "cosmos.ValidatorAddressString"];
  // host is the network address for the fibre service provider
  string host = 2;
}
```

Validation rules:

1. `signer` must be a valid `celestiavaloper` address, and a validator with that operator address must exist in staking state. The transaction is signed by the validator's account key (the account address underlying the operator address).
1. `host` must be in `host:port` form: a non-empty host part (IP literal or DNS name — both work, see the [transport security notes](../../fibre/cmd/README.md#transport-security-tls)) followed by a numeric port in the range [1, 65535]. Schemes (`http://`, `dns:///`, …) and URL paths are rejected.
1. `host` must be at most 100 characters.

The record is stored under the validator's **consensus** address, derived on-chain from the validator's consensus public key — the submitting operator never provides it directly.

## Events

### `EventSetFibreProviderInfo`

| Attribute Key               | Attribute Value                                 |
|-----------------------------|-------------------------------------------------|
| validator_consensus_address | {bech32 `celestiavalcons` address}              |
| host                        | {registered host in `host:port` form}           |

## Usage

Register (or update) your fibre host, signing with the validator's account key:

```shell
celestia-appd tx valaddr set-host 203.0.113.7:7980 --from <validator-account-key>
```

Verify the registration:

```shell
celestia-appd query valaddr provider <celestiavalcons-address>
```

List the registered providers of all currently bonded validators (entries whose validator has left the active set are omitted):

```shell
celestia-appd query valaddr providers
```

The same queries are available over gRPC (`celestia.valaddr.v1.Query`) and REST (`/valaddr/v1/fibre-provider-info/{validator_consensus_address}`, `/valaddr/v1/all-bonded-fibre-providers`).

For the end-to-end operator path — running the fibre server itself and then registering it here — see [fibre/cmd/README.md](../../fibre/cmd/README.md). For the protocol-level specification of the registry, see [specs/src/fibre_registry_module.md](../../specs/src/fibre_registry_module.md).
